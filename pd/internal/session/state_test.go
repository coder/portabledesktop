package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStateRoundTrip(t *testing.T) {
	t.Parallel()

	xvncPid := 1234
	openboxPid := 5678

	original := StoredDesktopState{
		RuntimeDir:        "/run/user/1000",
		Display:           42,
		VNCPort:           5942,
		Geometry:          "1920x1080",
		Depth:             24,
		DPI:               120,
		DesktopSizeMode:   "fixed",
		SessionDir:        "/tmp/session-abc",
		CleanupSessionDir: true,
		XvncPid:           &xvncPid,
		OpenboxPid:        &openboxPid,
		Detached:          true,
		StateFile:         "/tmp/state.json",
		StartedAt:         "2025-01-15T10:30:00Z",
	}

	path := filepath.Join(t.TempDir(), "state.json")

	require.NoError(t, SaveState(path, original))

	loaded, err := LoadState(path)
	require.NoError(t, err)

	assert.Equal(t, original.RuntimeDir, loaded.RuntimeDir)
	assert.Equal(t, original.Display, loaded.Display)
	assert.Equal(t, original.VNCPort, loaded.VNCPort)
	assert.Equal(t, original.Geometry, loaded.Geometry)
	assert.Equal(t, original.Depth, loaded.Depth)
	assert.Equal(t, original.DPI, loaded.DPI)
	assert.Equal(t, original.DesktopSizeMode, loaded.DesktopSizeMode)
	assert.Equal(t, original.SessionDir, loaded.SessionDir)
	assert.Equal(t, original.CleanupSessionDir, loaded.CleanupSessionDir)
	assert.Equal(t, original.Detached, loaded.Detached)
	assert.Equal(t, original.StateFile, loaded.StateFile)
	assert.Equal(t, original.StartedAt, loaded.StartedAt)

	require.NotNil(t, loaded.XvncPid)
	assert.Equal(t, *original.XvncPid, *loaded.XvncPid)

	require.NotNil(t, loaded.OpenboxPid)
	assert.Equal(t, *original.OpenboxPid, *loaded.OpenboxPid)
}

func TestStateRead_MissingFields(t *testing.T) {
	t.Parallel()

	// JSON with no dpi field — LoadState should default it to 96.
	data := []byte(`{"runtimeDir": "/run/user/1000", "display": 1}`)

	path := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, os.WriteFile(path, data, 0o644))

	state, err := LoadState(path)
	require.NoError(t, err)

	assert.Equal(t, 96, state.DPI)
}

func TestStateRead_CorruptJSON(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, os.WriteFile(path, []byte("{not json!!!"), 0o644))

	_, err := LoadState(path)
	assert.Error(t, err)
}

func TestStateRead_MissingFile(t *testing.T) {
	t.Parallel()

	_, err := LoadState(filepath.Join(t.TempDir(), "does-not-exist.json"))
	assert.Error(t, err)
}

func TestDefaultStateFilePath_EnvSet(t *testing.T) {
	t.Setenv("PORTABLEDESKTOP_STATE_FILE", "/custom/path/state.json")

	assert.Equal(t, "/custom/path/state.json", DefaultStateFilePath())
}

func TestDefaultStateFilePath_EnvUnset(t *testing.T) {
	t.Setenv("PORTABLEDESKTOP_STATE_FILE", "")
	t.Setenv("XDG_CACHE_HOME", "")
	// HOME must be set so the fallback is deterministic.
	t.Setenv("HOME", "/home/testuser")

	path := DefaultStateFilePath()
	assert.True(t, strings.HasSuffix(path, ".cache/portabledesktop/session.json"),
		"expected path to end with .cache/portabledesktop/session.json, got %s", path)
}
