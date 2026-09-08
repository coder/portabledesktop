package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ValidateRuntimeDir checks that dir contains a bin/Xvnc binary.
func ValidateRuntimeDir(dir string) error {
	xvnc := filepath.Join(dir, "bin", "Xvnc")
	if _, err := os.Stat(xvnc); err != nil {
		return fmt.Errorf(
			"runtime dir %q invalid: bin/Xvnc not found: %w",
			dir, err,
		)
	}
	return nil
}

// ResolveRuntimeBinary returns the full path to name inside
// runtimeDir/bin if the file exists there. Otherwise it falls back
// to returning name unqualified so the caller can rely on PATH
// resolution.
func ResolveRuntimeBinary(runtimeDir, name string) string {
	p := filepath.Join(runtimeDir, "bin", name)
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return name
}

// ResolveRuntimeData returns the full path to a file under
// runtimeDir/share/<subpath> if it exists. Returns an empty
// string if the file is not found.
func ResolveRuntimeData(runtimeDir, subpath string) string {
	p := filepath.Join(runtimeDir, "share", subpath)
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

// HasBinary reports whether name is available either inside runtimeDir/bin
// or on PATH. Slim runtimes ship only Xvnc and xkbcomp, so callers use this
// to skip optional components such as the window manager.
func HasBinary(runtimeDir, name string) bool {
	if _, err := os.Stat(filepath.Join(runtimeDir, "bin", name)); err == nil {
		return true
	}
	_, err := exec.LookPath(name)
	return err == nil
}

// XKBDir returns the keymap data directory to pass to Xvnc as -xkbdir, or
// an empty string when the runtime relies on its own wrapper to set it. The
// slim runtime keeps keymaps under share/xkb and ships an unwrapped Xvnc.
func XKBDir(runtimeDir string) string {
	p := filepath.Join(runtimeDir, "share", "xkb")
	if info, err := os.Stat(p); err == nil && info.IsDir() {
		return p
	}
	return ""
}

// ErrToolUnavailable is returned when a command needs a tool that is neither
// bundled in the runtime nor installed on the host.
type ErrToolUnavailable struct {
	Tool string
}

func (e *ErrToolUnavailable) Error() string {
	return fmt.Sprintf("%s is not bundled in this runtime and was not found on PATH", e.Tool)
}
