package desktop

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestParseDesktopEntry(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "firefox.desktop")
	writeDesktopEntry(t, path, strings.Join([]string{
		"[Desktop Entry]",
		"Name=Firefox",
		"Exec=firefox %u",
		"TryExec=firefox",
		"Icon=firefox",
		"Type=Application",
		"OnlyShowIn=OPENBOX;",
	}, "\n")+"\n")

	app, err := parseDesktopEntry(path)
	if err != nil {
		t.Fatalf("parseDesktopEntry() error = %v", err)
	}
	if app.Name != "Firefox" || app.Exec != "firefox %u" || app.TryExec != "firefox" {
		t.Fatalf("unexpected app contents: %+v", app)
	}
	if app.DesktopID != "firefox.desktop" {
		t.Fatalf("DesktopID = %q, want firefox.desktop", app.DesktopID)
	}
}

func TestFindApplicationsFiltersAndSorts(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	appsDir := filepath.Join(home, ".local", "share", "applications")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin dir: %v", err)
	}
	if err := os.MkdirAll(appsDir, 0o755); err != nil {
		t.Fatalf("mkdir apps dir: %v", err)
	}

	writeExecutable(t, filepath.Join(binDir, "firefox"))
	writeExecutable(t, filepath.Join(binDir, "custom-browser"))
	writeExecutable(t, filepath.Join(binDir, "xterm"))

	t.Setenv("HOME", home)
	t.Setenv("PATH", binDir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_DATA_DIRS", filepath.Join(home, "share"))

	writeDesktopEntry(t, filepath.Join(appsDir, "google-chrome.desktop"), desktopEntry([]string{
		"Name=Chrome",
		"Exec=custom-browser",
		"Type=Application",
	}))
	writeDesktopEntry(t, filepath.Join(appsDir, "firefox.desktop"), desktopEntry([]string{
		"Name=Firefox",
		"Exec=firefox",
		"TryExec=firefox",
		"Type=Application",
	}))
	writeDesktopEntry(t, filepath.Join(appsDir, "xterm.desktop"), desktopEntry([]string{
		"Name=XTerm",
		"Exec=xterm",
		"Type=Application",
	}))
	writeDesktopEntry(t, filepath.Join(appsDir, "hidden.desktop"), desktopEntry([]string{
		"Name=Hidden",
		"Exec=xterm",
		"Type=Application",
		"Hidden=true",
	}))
	writeDesktopEntry(t, filepath.Join(appsDir, "nodisplay.desktop"), desktopEntry([]string{
		"Name=NoDisplay",
		"Exec=xterm",
		"Type=Application",
		"NoDisplay=true",
	}))
	writeDesktopEntry(t, filepath.Join(appsDir, "missing-tryexec.desktop"), desktopEntry([]string{
		"Name=Missing TryExec",
		"Exec=missing-cmd",
		"TryExec=missing-cmd",
		"Type=Application",
	}))
	writeDesktopEntry(t, filepath.Join(appsDir, "terminal.desktop"), desktopEntry([]string{
		"Name=Terminal Tool",
		"Exec=xterm -e htop",
		"Type=Application",
		"Terminal=true",
	}))
	writeDesktopEntry(t, filepath.Join(appsDir, "wrong-onlyshowin.desktop"), desktopEntry([]string{
		"Name=Wrong OnlyShowIn",
		"Exec=xterm",
		"Type=Application",
		"OnlyShowIn=GNOME;",
	}))
	writeDesktopEntry(t, filepath.Join(appsDir, "blocked-notshowin.desktop"), desktopEntry([]string{
		"Name=Blocked",
		"Exec=xterm",
		"Type=Application",
		"NotShowIn=OPENBOX;",
	}))
	writeDesktopEntry(t, filepath.Join(appsDir, "duplicate-firefox.desktop"), desktopEntry([]string{
		"Name=Firefox Duplicate",
		"Exec=firefox",
		"Type=Application",
	}))

	apps, err := FindApplications()
	if err != nil {
		t.Fatalf("FindApplications() error = %v", err)
	}

	gotIDs := make([]string, 0, len(apps))
	for _, app := range apps {
		gotIDs = append(gotIDs, app.DesktopID)
	}
	wantIDs := []string{"google-chrome.desktop", "firefox.desktop", "xterm.desktop"}
	if strings.Join(gotIDs, ",") != strings.Join(wantIDs, ",") {
		t.Fatalf("DesktopIDs = %v, want %v", gotIDs, wantIDs)
	}
}

func TestFindApplicationsCapsLauncherCount(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	appsDir := filepath.Join(home, ".local", "share", "applications")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin dir: %v", err)
	}
	if err := os.MkdirAll(appsDir, 0o755); err != nil {
		t.Fatalf("mkdir apps dir: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", binDir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_DATA_DIRS", filepath.Join(home, "share"))

	for i := 0; i < maxAutoLaunchers+2; i++ {
		binary := "tool-" + strconv.Itoa(i)
		writeExecutable(t, filepath.Join(binDir, binary))
		path := filepath.Join(appsDir, "app-"+strconv.Itoa(i)+".desktop")
		writeDesktopEntry(t, path, desktopEntry([]string{
			"Name=App " + strconv.Itoa(i),
			"Exec=" + binary,
			"Type=Application",
		}))
	}

	apps, err := FindApplications()
	if err != nil {
		t.Fatalf("FindApplications() error = %v", err)
	}
	if len(apps) != maxAutoLaunchers {
		t.Fatalf("len(apps) = %d, want %d", len(apps), maxAutoLaunchers)
	}
}

func desktopEntry(lines []string) string {
	return "[Desktop Entry]\n" + strings.Join(lines, "\n") + "\n"
}

func writeDesktopEntry(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir desktop entry dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write desktop entry: %v", err)
	}
}

func writeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}
}
