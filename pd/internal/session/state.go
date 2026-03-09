package session

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// StoredDesktopState holds the persistent state of a desktop session.
// It is serialized to JSON on disk so the CLI can stop or inspect a
// session that was started in a previous invocation.
type StoredDesktopState struct {
	RuntimeDir        string `json:"runtimeDir"`
	Display           int    `json:"display"`
	VNCPort           int    `json:"vncPort"`
	Geometry          string `json:"geometry"`
	Depth             int    `json:"depth"`
	DPI               int    `json:"dpi"`
	DesktopSizeMode   string `json:"desktopSizeMode"`
	SessionDir        string `json:"sessionDir"`
	CleanupSessionDir bool   `json:"cleanupSessionDirOnStop"`
	XvncPid           *int   `json:"xvncPid"`
	OpenboxPid        *int   `json:"openboxPid"`
	Detached          bool   `json:"detached"`
	StateFile         string `json:"stateFile"`
	StartedAt         string `json:"startedAt"`
}

// SaveState writes state as indented JSON to path, creating any
// parent directories that do not already exist.
func SaveState(path string, state StoredDesktopState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	// Append a trailing newline so the file is POSIX-friendly.
	data = append(data, '\n')

	return os.WriteFile(path, data, 0o644)
}

// LoadState reads and parses a JSON state file from path. If the
// DPI field is zero (missing or explicitly set to 0), it defaults
// to 96.
func LoadState(path string) (StoredDesktopState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return StoredDesktopState{}, err
	}

	var state StoredDesktopState
	if err := json.Unmarshal(data, &state); err != nil {
		return StoredDesktopState{}, err
	}

	if state.DPI == 0 {
		state.DPI = 96
	}

	return state, nil
}

// DefaultStateFilePath returns the path used for the session state
// file. The PORTABLEDESKTOP_STATE_FILE environment variable takes
// precedence; otherwise the path falls back to
// <xdgCacheHome>/portabledesktop/session.json.
func DefaultStateFilePath() string {
	if v := os.Getenv("PORTABLEDESKTOP_STATE_FILE"); v != "" {
		return v
	}

	cacheHome := os.Getenv("XDG_CACHE_HOME")
	if cacheHome == "" {
		cacheHome = filepath.Join(os.Getenv("HOME"), ".cache")
	}

	return filepath.Join(cacheHome, "portabledesktop", "session.json")
}
