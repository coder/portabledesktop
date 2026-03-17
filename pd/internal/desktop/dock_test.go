package desktop

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPatchGdkPixbufCache(t *testing.T) {
	t.Parallel()

	runtimeDir := t.TempDir()
	sessionDir := t.TempDir()
	cacheDir := filepath.Join(runtimeDir, "lib", "gdk-pixbuf-2.0", "2.10.0")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}

	src := filepath.Join(cacheDir, "loaders.cache")
	contents := "\"@@RUNTIME_LIB@@/gdk-pixbuf-2.0/2.10.0/loaders/libpixbufloader_svg.so\"\n"
	if err := os.WriteFile(src, []byte(contents), 0o644); err != nil {
		t.Fatalf("write source cache: %v", err)
	}

	patchedPath, err := PatchGdkPixbufCache(runtimeDir, sessionDir)
	if err != nil {
		t.Fatalf("PatchGdkPixbufCache() error = %v", err)
	}

	patchedContents, err := os.ReadFile(patchedPath)
	if err != nil {
		t.Fatalf("read patched cache: %v", err)
	}

	wantFragment := filepath.ToSlash(filepath.Join(runtimeDir, "lib", "gdk-pixbuf-2.0"))
	if !strings.Contains(string(patchedContents), wantFragment) {
		t.Fatalf("patched cache did not contain runtime loader path %q: %s", wantFragment, patchedContents)
	}
	if strings.Contains(string(patchedContents), gdkPixbufRuntimePlaceholder) {
		t.Fatalf("patched cache still contains runtime placeholder: %s", patchedContents)
	}
}

func TestSetupPlankConfigUsesProvidedApps(t *testing.T) {
	t.Parallel()

	tempAppsDir := t.TempDir()
	firefox := filepath.Join(tempAppsDir, "firefox.desktop")
	chromium := filepath.Join(tempAppsDir, "chromium.desktop")
	writeLauncherDesktopFile(t, firefox)
	writeLauncherDesktopFile(t, chromium)

	sessionDir := t.TempDir()
	config, err := SetupPlankConfig(sessionDir, []DesktopApp{
		{DesktopID: "firefox.desktop", DesktopFile: firefox},
		{DesktopID: "chromium.desktop", DesktopFile: chromium},
	})
	if err != nil {
		t.Fatalf("SetupPlankConfig() error = %v", err)
	}

	if config.DockName != dockName {
		t.Fatalf("DockName = %q, want %q", config.DockName, dockName)
	}

	wantLaunchers := []string{"firefox.dockitem", "chromium.dockitem"}
	if got := strings.Join(config.LauncherNames, ","); got != strings.Join(wantLaunchers, ",") {
		t.Fatalf("LauncherNames = %v, want %v", config.LauncherNames, wantLaunchers)
	}

	launchersDir := filepath.Join(config.ConfigHome, "plank", dockName, "launchers")
	for _, launcherName := range wantLaunchers {
		launcherPath := filepath.Join(launchersDir, launcherName)
		contents, err := os.ReadFile(launcherPath)
		if err != nil {
			t.Fatalf("read launcher %q: %v", launcherName, err)
		}
		if !strings.Contains(string(contents), "Launcher=file://") {
			t.Fatalf("launcher %q missing file URL: %s", launcherName, contents)
		}
	}

	keyfilePath := filepath.Join(config.ConfigHome, gsettingsKeyfileRelativePath)
	keyfileContents, err := os.ReadFile(keyfilePath)
	if err != nil {
		t.Fatalf("read keyfile: %v", err)
	}
	keyfile := string(keyfileContents)
	if !strings.Contains(keyfile, "enabled-docks=['dock1']") {
		t.Fatalf("keyfile missing enabled docks setting: %s", keyfile)
	}
	if !strings.Contains(keyfile, "dock-items=['firefox.dockitem', 'chromium.dockitem']") {
		t.Fatalf("keyfile missing dock items setting: %s", keyfile)
	}
}

func TestStartDockReturnsErrorWhenWrapperExitsImmediately(t *testing.T) {
	runtimeDir := t.TempDir()
	sessionDir := t.TempDir()
	xdgDataHome := t.TempDir()
	xdgDataDirs := t.TempDir()

	t.Setenv("XDG_DATA_HOME", xdgDataHome)
	t.Setenv("XDG_DATA_DIRS", xdgDataDirs)

	launcherPath := filepath.Join(xdgDataHome, "applications", "test.desktop")
	writeLauncherDesktopFile(t, launcherPath)

	cacheDir := filepath.Join(runtimeDir, "lib", "gdk-pixbuf-2.0", "2.10.0")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	cachePath := filepath.Join(cacheDir, "loaders.cache")
	cacheContents := "\"@@RUNTIME_LIB@@/gdk-pixbuf-2.0/2.10.0/loaders/libpixbufloader_svg.so\"\n"
	if err := os.WriteFile(cachePath, []byte(cacheContents), 0o644); err != nil {
		t.Fatalf("write gdk-pixbuf cache: %v", err)
	}

	plankPath := filepath.Join(runtimeDir, "bin", "plank")
	if err := os.MkdirAll(filepath.Dir(plankPath), 0o755); err != nil {
		t.Fatalf("mkdir plank bin dir: %v", err)
	}
	plankWrapper := `#!/bin/sh
real="$(dirname "$0")/plank.real"
if [ ! -x "$real" ]; then
  echo "error: missing plank payload binary: $real" >&2
  exit 1
fi
exec "$real" "$@"
`
	if err := os.WriteFile(plankPath, []byte(plankWrapper), 0o755); err != nil {
		t.Fatalf("write plank wrapper: %v", err)
	}

	pid, err := StartDock(runtimeDir, 99, sessionDir, false)
	if err == nil {
		t.Fatal("StartDock() error = nil, want immediate-exit error")
	}
	if pid != 0 {
		t.Fatalf("StartDock() pid = %d, want 0 when startup fails", pid)
	}
	if !strings.Contains(err.Error(), "plank exited during startup") {
		t.Fatalf("StartDock() error = %v, want startup-exit message", err)
	}

	logContents, readErr := os.ReadFile(filepath.Join(sessionDir, dockLogFileName))
	if readErr != nil {
		t.Fatalf("read dock log: %v", readErr)
	}
	logText := string(logContents)
	if !strings.Contains(logText, "starting dock with 1 launcher(s)") {
		t.Fatalf("dock log missing startup preamble: %s", logText)
	}
	if !strings.Contains(logText, "missing plank payload binary") {
		t.Fatalf("dock log missing wrapper failure: %s", logText)
	}
}

func TestDesktopKillStopsDockPid(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep process: %v", err)
	}

	d := &Desktop{DockPid: cmd.Process.Pid}
	if err := d.Kill(KillOptions{Cleanup: BoolPtr(false)}); err != nil {
		t.Fatalf("Kill() error = %v", err)
	}
	if d.DockPid != 0 {
		t.Fatalf("DockPid = %d, want 0 after Kill()", d.DockPid)
	}

	if err := cmd.Wait(); err == nil {
		t.Fatalf("dock process unexpectedly exited cleanly after Kill()")
	}
}

func writeLauncherDesktopFile(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir desktop file parent %q: %v", path, err)
	}
	if err := os.WriteFile(path, []byte("[Desktop Entry]\nName=Test\nExec=true\nType=Application\n"), 0o644); err != nil {
		t.Fatalf("write launcher %q: %v", path, err)
	}
}
