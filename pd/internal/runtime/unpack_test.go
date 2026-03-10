package runtime

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestTarZst builds a small synthetic .tar.zst in-memory.
// Every file whose path starts with "bin/" gets mode 0755;
// everything else gets 0644.
func createTestTarZst(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)

	// Collect and sort directory entries we need to emit so the
	// archive is self-contained.
	dirs := make(map[string]struct{})
	for name := range files {
		dir := filepath.Dir(name)
		for dir != "." && dir != "/" {
			dirs[dir] = struct{}{}
			dir = filepath.Dir(dir)
		}
	}
	for d := range dirs {
		err := tw.WriteHeader(&tar.Header{
			Name:     d + "/",
			Typeflag: tar.TypeDir,
			Mode:     0o755,
		})
		require.NoError(t, err)
	}

	for name, content := range files {
		mode := int64(0o644)
		if len(name) >= 4 && name[:4] == "bin/" {
			mode = 0o755
		}
		err := tw.WriteHeader(&tar.Header{
			Name:     name,
			Size:     int64(len(content)),
			Mode:     mode,
			Typeflag: tar.TypeReg,
		})
		require.NoError(t, err)

		_, err = tw.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())

	enc, err := zstd.NewWriter(nil)
	require.NoError(t, err)
	defer enc.Close()

	return enc.EncodeAll(tarBuf.Bytes(), nil)
}

func TestEnsureRuntime_FreshUnpack(t *testing.T) {
	blob := createTestTarZst(t, map[string]string{
		"bin/Xvnc":   "fake-xvnc",
		"bin/helper":  "helper-bin",
		"lib/libx.so": "shared-obj",
	})

	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	t.Setenv("PORTABLEDESKTOP_RUNTIME_DIR", "")

	dir, err := EnsureRuntime(blob)
	require.NoError(t, err)
	require.DirExists(t, dir)

	// Verify cache dir name contains hash prefix.
	digest := sha256.Sum256(blob)
	shortHex := hex.EncodeToString(digest[:])[:12]
	assert.Contains(t, filepath.Base(dir), shortHex)

	// Verify file content.
	data, err := os.ReadFile(filepath.Join(dir, "bin", "Xvnc"))
	require.NoError(t, err)
	assert.Equal(t, "fake-xvnc", string(data))

	// Verify executable bit.
	info, err := os.Stat(filepath.Join(dir, "bin", "Xvnc"))
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&0o111, "bin/Xvnc should be executable")
}

func TestEnsureRuntime_CachedHit(t *testing.T) {
	blob := createTestTarZst(t, map[string]string{
		"bin/Xvnc": "xvnc",
	})

	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	t.Setenv("PORTABLEDESKTOP_RUNTIME_DIR", "")

	dir1, err := EnsureRuntime(blob)
	require.NoError(t, err)

	dir2, err := EnsureRuntime(blob)
	require.NoError(t, err)

	assert.Equal(t, dir1, dir2)
}

func TestEnsureRuntime_DifferentBlob(t *testing.T) {
	blob1 := createTestTarZst(t, map[string]string{
		"bin/Xvnc": "xvnc-v1",
	})
	blob2 := createTestTarZst(t, map[string]string{
		"bin/Xvnc": "xvnc-v2",
	})

	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	t.Setenv("PORTABLEDESKTOP_RUNTIME_DIR", "")

	dir1, err := EnsureRuntime(blob1)
	require.NoError(t, err)

	dir2, err := EnsureRuntime(blob2)
	require.NoError(t, err)

	// Different blobs should produce different cache dirs.
	assert.NotEqual(t, dir1, dir2)

	// Both should contain their respective content.
	data1, err := os.ReadFile(filepath.Join(dir1, "bin", "Xvnc"))
	require.NoError(t, err)
	assert.Equal(t, "xvnc-v1", string(data1))

	data2, err := os.ReadFile(filepath.Join(dir2, "bin", "Xvnc"))
	require.NoError(t, err)
	assert.Equal(t, "xvnc-v2", string(data2))
}

func TestEnsureRuntime_ConcurrentUnpack(t *testing.T) {
	blob := createTestTarZst(t, map[string]string{
		"bin/Xvnc": "xvnc",
	})

	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	t.Setenv("PORTABLEDESKTOP_RUNTIME_DIR", "")

	const n = 10
	var wg sync.WaitGroup
	dirs := make([]string, n)
	errs := make([]error, n)

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			dirs[idx], errs[idx] = EnsureRuntime(blob)
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		require.NoError(t, errs[i], "goroutine %d", i)
		assert.Equal(t, dirs[0], dirs[i], "goroutine %d returned different dir", i)
	}
}

func TestEnsureRuntime_ExplicitOverride(t *testing.T) {
	overrideDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(overrideDir, "bin"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(overrideDir, "bin", "Xvnc"), []byte("x"), 0o755,
	))

	t.Setenv("PORTABLEDESKTOP_RUNTIME_DIR", overrideDir)

	dir, err := EnsureRuntime(nil)
	require.NoError(t, err)
	assert.Equal(t, overrideDir, dir)
}

func TestEnsureRuntime_ExplicitOverride_MissingXvnc(t *testing.T) {
	emptyDir := t.TempDir()
	t.Setenv("PORTABLEDESKTOP_RUNTIME_DIR", emptyDir)

	_, err := EnsureRuntime(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bin/Xvnc")
}

func TestEnsureRuntime_EmptyBlob(t *testing.T) {
	t.Setenv("PORTABLEDESKTOP_RUNTIME_DIR", "")

	_, err := EnsureRuntime(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")

	_, err = EnsureRuntime([]byte{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestXdgCacheHome_EnvSet(t *testing.T) {
	want := "/custom/cache"
	t.Setenv("XDG_CACHE_HOME", want)

	assert.Equal(t, want, xdgCacheHome())
}

func TestXdgCacheHome_EnvUnset_FallsBackToHomeCache(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "/fakehome")

	assert.Equal(t, "/fakehome/.cache", xdgCacheHome())
}

func TestValidateRuntimeDir_Valid(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "bin"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "bin", "Xvnc"), []byte("x"), 0o755,
	))

	assert.NoError(t, ValidateRuntimeDir(dir))
}

func TestValidateRuntimeDir_MissingXvnc(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	err := ValidateRuntimeDir(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bin/Xvnc")
}

func TestResolveRuntimeBinary_Found(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(binDir, "mybin"), []byte("x"), 0o755,
	))

	got := ResolveRuntimeBinary(dir, "mybin")
	assert.Equal(t, filepath.Join(dir, "bin", "mybin"), got)
}

func TestResolveRuntimeBinary_NotFound_FallsBackToName(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	got := ResolveRuntimeBinary(dir, "nonexistent")
	assert.Equal(t, "nonexistent", got)
}
