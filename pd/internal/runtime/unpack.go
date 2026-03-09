package runtime

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// Option is a functional option for EnsureRuntime.
type Option func(*options)

type options struct {
	runtimeDir string
}

// WithRuntimeDir overrides the PORTABLEDESKTOP_RUNTIME_DIR env var
// check with the given directory path.
func WithRuntimeDir(dir string) Option {
	return func(o *options) {
		o.runtimeDir = dir
	}
}

// xdgCacheHome returns $XDG_CACHE_HOME if set, otherwise falls
// back to $HOME/.cache.
func xdgCacheHome() string {
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return dir
	}
	return filepath.Join(os.Getenv("HOME"), ".cache")
}

// EnsureRuntime unpacks the embedded zstd-compressed tar runtime
// blob into an XDG cache directory and returns the path to it.
//
// If PORTABLEDESKTOP_RUNTIME_DIR (or the WithRuntimeDir option) is
// set, that directory is validated and returned directly without
// extracting anything.
//
// The cache key is derived from the first 12 hex characters of the
// SHA-256 digest of embeddedBlob. If the cache directory already
// exists it is reused immediately; otherwise the blob is extracted
// into a temporary directory and atomically renamed into place.
func EnsureRuntime(embeddedBlob []byte, opts ...Option) (string, error) {
	var o options
	for _, fn := range opts {
		fn(&o)
	}

	// Check explicit override first: option, then env var.
	explicitDir := o.runtimeDir
	if explicitDir == "" {
		explicitDir = os.Getenv("PORTABLEDESKTOP_RUNTIME_DIR")
	}
	if explicitDir != "" {
		if err := ValidateRuntimeDir(explicitDir); err != nil {
			return "", fmt.Errorf("explicit runtime dir: %w", err)
		}
		return explicitDir, nil
	}

	if len(embeddedBlob) == 0 {
		return "", fmt.Errorf("embedded runtime blob is empty")
	}

	// Compute SHA-256 of the blob.
	digest := sha256.Sum256(embeddedBlob)
	fullHex := hex.EncodeToString(digest[:])
	shortHex := fullHex[:12]

	cacheDir := filepath.Join(
		xdgCacheHome(), "portabledesktop", "runtime-"+shortHex,
	)

	// Fast path: directory already exists — reuse immediately.
	if info, err := os.Stat(cacheDir); err == nil && info.IsDir() {
		return cacheDir, nil
	}

	// Slow path: extract into a temporary directory, then rename
	// atomically into place.
	tmpDir := fmt.Sprintf(
		"%s.tmp-%d-%d", cacheDir, os.Getpid(), rand.Int63(), //nolint:gosec
	)
	// Clean up any stale tmp dir from a previous crashed run.
	_ = os.RemoveAll(tmpDir)

	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return "", fmt.Errorf("create tmp dir: %w", err)
	}
	// Ensure cleanup of tmpDir on all exit paths below.
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	if err := extractTarZst(embeddedBlob, tmpDir); err != nil {
		return "", fmt.Errorf("extract runtime: %w", err)
	}

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(cacheDir), 0o755); err != nil {
		return "", fmt.Errorf("create cache parent dir: %w", err)
	}

	if err := os.Rename(tmpDir, cacheDir); err != nil {
		// Another process may have won the race and already
		// placed the directory. If it exists, reuse it.
		if info, statErr := os.Stat(cacheDir); statErr == nil && info.IsDir() {
			// removeTmp stays true — defer will clean up tmpDir.
			return cacheDir, nil
		}
		return "", fmt.Errorf("rename tmp to cache dir: %w", err)
	}

	// Rename succeeded — don't remove tmpDir (it's now cacheDir).
	removeTmp = false
	return cacheDir, nil
}

// extractTarZst decompresses a zstd-compressed tar archive from src
// into dstDir, preserving file permissions, directories, and
// symlinks.
func extractTarZst(src []byte, dstDir string) error {
	zr, err := zstd.NewReader(bytes.NewReader(src))
	if err != nil {
		return fmt.Errorf("new zstd reader: %w", err)
	}
	defer zr.Close()

	tr := tar.NewReader(zr)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar next: %w", err)
		}

		// Guard against path traversal.
		target := filepath.Join(dstDir, hdr.Name) //nolint:gosec
		if !strings.HasPrefix(
			filepath.Clean(target),
			filepath.Clean(dstDir)+string(os.PathSeparator),
		) && filepath.Clean(target) != filepath.Clean(dstDir) {
			return fmt.Errorf("tar entry %q escapes destination", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, hdr.FileInfo().Mode()); err != nil {
				return fmt.Errorf("mkdir %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("mkdir parent %s: %w", target, err)
			}
			if err := writeFile(target, tr, hdr.FileInfo().Mode()); err != nil {
				return err
			}
		case tar.TypeSymlink:
			// Validate symlink target for path traversal.
			linkTarget := hdr.Linkname
			if filepath.IsAbs(linkTarget) {
				absLink := filepath.Clean(linkTarget)
				if !strings.HasPrefix(absLink, filepath.Clean(dstDir)+string(os.PathSeparator)) &&
					absLink != filepath.Clean(dstDir) {
					return fmt.Errorf("symlink %q target %q escapes destination", hdr.Name, linkTarget)
				}
			} else {
				absLink := filepath.Clean(filepath.Join(filepath.Dir(target), linkTarget))
				if !strings.HasPrefix(absLink, filepath.Clean(dstDir)+string(os.PathSeparator)) &&
					absLink != filepath.Clean(dstDir) {
					return fmt.Errorf("symlink %q target %q escapes destination", hdr.Name, linkTarget)
				}
			}

			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("mkdir parent %s: %w", target, err)
			}
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return fmt.Errorf("symlink %s: %w", target, err)
			}
		default:
			// Skip unsupported entry types.
		}
	}

	return nil
}

// writeFile creates a regular file at path with the given mode and
// copies content from r into it.
func writeFile(path string, r io.Reader, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}

	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}
