package desktop

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type DesktopApp struct {
	DesktopID   string
	DesktopFile string
	Name        string
	Exec        string
	TryExec     string
	Icon        string
	Type        string
	Terminal    bool
	NoDisplay   bool
	Hidden      bool
	OnlyShowIn  []string
	NotShowIn   []string
}

var curatedDesktopIDs = []string{
	"google-chrome.desktop",
	"firefox.desktop",
	"chromium.desktop",
	"org.chromium.Chromium.desktop",
	"org.gnome.Terminal.desktop",
	"gnome-terminal.desktop",
	"org.kde.konsole.desktop",
	"konsole.desktop",
	"xterm.desktop",
}

const maxAutoLaunchers = 8

// FindApplications returns a bounded, de-duplicated set of desktop launchers.
func FindApplications() ([]DesktopApp, error) {
	entries, err := scanDesktopApplications()
	if err != nil {
		return nil, err
	}

	filtered := make([]DesktopApp, 0, len(entries))
	for _, app := range entries {
		if !isEligibleDesktopApp(app) {
			continue
		}
		filtered = append(filtered, app)
	}

	sortDesktopApps(filtered)
	filtered = dedupeDesktopApps(filtered)
	if len(filtered) > maxAutoLaunchers {
		filtered = filtered[:maxAutoLaunchers]
	}
	return filtered, nil
}

func scanDesktopApplications() ([]DesktopApp, error) {
	dirs := xdgApplicationDirs()
	seenPaths := make(map[string]struct{}, 64)
	apps := make([]DesktopApp, 0, 64)
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read applications dir %q: %w", dir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".desktop") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			if _, ok := seenPaths[path]; ok {
				continue
			}
			seenPaths[path] = struct{}{}

			app, err := parseDesktopEntry(path)
			if err != nil {
				continue
			}
			apps = append(apps, app)
		}
	}
	return apps, nil
}

func parseDesktopEntry(path string) (DesktopApp, error) {
	file, err := os.Open(path)
	if err != nil {
		return DesktopApp{}, fmt.Errorf("open desktop file: %w", err)
	}
	defer file.Close()

	app := DesktopApp{
		DesktopID:   filepath.Base(path),
		DesktopFile: path,
	}

	scanner := bufio.NewScanner(file)
	inDesktopEntry := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inDesktopEntry = line == "[Desktop Entry]"
			continue
		}
		if !inDesktopEntry {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "Name":
			if app.Name == "" {
				app.Name = value
			}
		case "Exec":
			app.Exec = value
		case "TryExec":
			app.TryExec = value
		case "Icon":
			app.Icon = value
		case "Type":
			app.Type = value
		case "Terminal":
			app.Terminal = parseDesktopBool(value)
		case "NoDisplay":
			app.NoDisplay = parseDesktopBool(value)
		case "Hidden":
			app.Hidden = parseDesktopBool(value)
		case "OnlyShowIn":
			app.OnlyShowIn = parseDesktopList(value)
		case "NotShowIn":
			app.NotShowIn = parseDesktopList(value)
		}
	}
	if err := scanner.Err(); err != nil {
		return DesktopApp{}, fmt.Errorf("scan desktop file: %w", err)
	}
	return app, nil
}

func isEligibleDesktopApp(app DesktopApp) bool {
	if app.Type != "Application" {
		return false
	}
	if app.Hidden || app.NoDisplay || app.Terminal {
		return false
	}
	if app.Name == "" || app.Exec == "" {
		return false
	}
	if !isVisibleInDesktopEnvironment(app) {
		return false
	}
	if app.TryExec != "" && !tryExecExists(app.TryExec) {
		return false
	}
	return true
}

func isVisibleInDesktopEnvironment(app DesktopApp) bool {
	const desktopEnv = "OPENBOX"
	if len(app.OnlyShowIn) > 0 && !containsStringFold(app.OnlyShowIn, desktopEnv) {
		return false
	}
	if containsStringFold(app.NotShowIn, desktopEnv) {
		return false
	}
	return true
}

func tryExecExists(value string) bool {
	cmd := strings.TrimSpace(value)
	if cmd == "" {
		return false
	}
	binary := desktopExecBinary(cmd)
	if binary == "" {
		return false
	}
	if strings.ContainsRune(binary, filepath.Separator) {
		_, err := os.Stat(binary)
		return err == nil
	}
	_, err := exec.LookPath(binary)
	return err == nil
}

func desktopExecBinary(value string) string {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func dedupeDesktopApps(apps []DesktopApp) []DesktopApp {
	seen := make(map[string]struct{}, len(apps))
	out := make([]DesktopApp, 0, len(apps))
	for _, app := range apps {
		key := dedupeKey(app)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, app)
	}
	return out
}

func dedupeKey(app DesktopApp) string {
	if execBinary := desktopExecBinary(app.Exec); execBinary != "" {
		return "exec:" + strings.ToLower(execBinary)
	}
	if app.DesktopID != "" {
		return "id:" + strings.ToLower(app.DesktopID)
	}
	return "path:" + strings.ToLower(app.DesktopFile)
}

func sortDesktopApps(apps []DesktopApp) {
	priority := make(map[string]int, len(curatedDesktopIDs))
	for i, desktopID := range curatedDesktopIDs {
		priority[strings.ToLower(desktopID)] = i
	}

	sort.SliceStable(apps, func(i, j int) bool {
		pi, iok := priority[strings.ToLower(apps[i].DesktopID)]
		pj, jok := priority[strings.ToLower(apps[j].DesktopID)]
		if iok && jok && pi != pj {
			return pi < pj
		}
		if iok != jok {
			return iok
		}
		if !strings.EqualFold(apps[i].Name, apps[j].Name) {
			return strings.ToLower(apps[i].Name) < strings.ToLower(apps[j].Name)
		}
		return strings.ToLower(apps[i].DesktopID) < strings.ToLower(apps[j].DesktopID)
	})
}

func xdgApplicationDirs() []string {
	var dirs []string
	if dataHome := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); dataHome != "" {
		dirs = append(dirs, filepath.Join(dataHome, "applications"))
	} else if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		dirs = append(dirs, filepath.Join(home, ".local", "share", "applications"))
	}

	dataDirs := os.Getenv("XDG_DATA_DIRS")
	if strings.TrimSpace(dataDirs) == "" {
		dataDirs = "/usr/local/share:/usr/share"
	}
	for _, dir := range splitPathList(dataDirs) {
		dirs = append(dirs, filepath.Join(dir, "applications"))
	}
	return uniqueStrings(dirs)
}

func parseDesktopBool(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "true")
}

func parseDesktopList(value string) []string {
	parts := strings.Split(value, ";")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func containsStringFold(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(value, want) {
			return true
		}
	}
	return false
}
