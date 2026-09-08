package desktop

import (
	"bufio"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pdruntime "github.com/coder/portabledesktop/pd/internal/runtime"
)

// slimRuntimeArchive returns the committed minimal runtime archive for this
// platform, or skips the test when there is none.
func slimRuntimeArchive(t *testing.T) []byte {
	t.Helper()
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		t.Skip("slim runtime is only built for linux amd64 and arm64")
	}
	// The test runs from pd/internal/desktop; the archives live at the
	// repository root under runtime/.
	path := filepath.Join("..", "..", "..", "runtime", "desktop-runtime-linux-"+runtime.GOARCH+".tar.zst")
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("slim runtime archive not found at %s: %v", path, err)
	}
	return blob
}

// TestStart_SlimRuntime starts a desktop from the minimal runtime, which has
// no window manager, wallpaper tools, xdotool or ffmpeg, and checks that the
// X server comes up and the optional pieces degrade cleanly.
func TestStart_SlimRuntime(t *testing.T) {
	blob := slimRuntimeArchive(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("PORTABLEDESKTOP_RUNTIME_DIR", "")
	// Make sure host tools do not mask a missing bundled tool.
	t.Setenv("PATH", "")

	runtimeDir, err := pdruntime.EnsureRuntime(blob)
	require.NoError(t, err)
	require.NotEmpty(t, pdruntime.XKBDir(runtimeDir), "slim runtime must expose share/xkb")

	d, err := Start(runtimeDir, StartOptions{
		SessionDir: t.TempDir(),
		Geometry:   "640x480",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Kill(KillOptions{}) })

	require.Zero(t, d.OpenboxPid, "openbox is not bundled and must be skipped")
	require.Zero(t, d.DockPid, "plank is not bundled and must be skipped")

	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(d.VNCPort)), 5*time.Second)
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	banner, err := bufio.NewReader(conn).ReadString('\n')
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(banner, "RFB 003."), "unexpected banner %q", banner)

	xvncLog, err := os.ReadFile(filepath.Join(d.SessionDir, "xvnc.log"))
	require.NoError(t, err)
	require.NotContains(t, string(xvncLog), "Failed to compile keymap")
	require.NotContains(t, string(xvncLog), "Fatal server error")

	// Tools that are not bundled fail with a typed, descriptive error.
	var unavailable *pdruntime.ErrToolUnavailable
	err = d.MoveMouse(1, 1)
	require.True(t, errors.As(err, &unavailable), "got %v", err)
	require.Equal(t, "xdotool", unavailable.Tool)
	_, err = d.Screenshot(ScreenshotOptions{})
	require.True(t, errors.As(err, &unavailable), "got %v", err)
	require.Equal(t, "ffmpeg", unavailable.Tool)
}
