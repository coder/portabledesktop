package runtime

import (
	"fmt"
	"os"
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
