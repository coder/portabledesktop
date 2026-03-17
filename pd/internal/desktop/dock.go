package desktop

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/coder/portabledesktop/pd/internal/runtime"
)

const (
	dockName                     = "dock1"
	dockLogFileName              = "dock.log"
	dockStartupGracePeriod       = 500 * time.Millisecond
	gdkPixbufRuntimePlaceholder  = "@@RUNTIME_LIB@@"
	gsettingsKeyfileRelativePath = "glib-2.0/settings/keyfile"
)

type dockLauncherSpec struct {
	DockItemName string
	DesktopFile  string
}

type plankConfig struct {
	DockName      string
	ConfigHome    string
	LauncherNames []string
}

// PatchGdkPixbufCache copies the packaged runtime loaders.cache into the
// session directory and rewrites runtime placeholders to the extracted runtime
// path.
func PatchGdkPixbufCache(runtimeDir, sessionDir string) (string, error) {
	src, err := findRuntimeGdkPixbufCache(runtimeDir)
	if err != nil {
		return "", err
	}

	contents, err := os.ReadFile(src)
	if err != nil {
		return "", fmt.Errorf("read packaged gdk-pixbuf cache: %w", err)
	}

	runtimeLibDir := filepath.ToSlash(filepath.Join(runtimeDir, "lib"))
	patched := strings.ReplaceAll(string(contents), gdkPixbufRuntimePlaceholder, runtimeLibDir)

	dst := filepath.Join(sessionDir, "gdk-pixbuf-loaders.cache")
	if err := os.WriteFile(dst, []byte(patched), 0o644); err != nil {
		return "", fmt.Errorf("write patched gdk-pixbuf cache: %w", err)
	}

	return dst, nil
}

// SetupPlankConfig creates an isolated Plank configuration rooted in the
// session directory and seeds launcher entries for the provided apps.
func SetupPlankConfig(sessionDir string, apps []DesktopApp) (plankConfig, error) {
	configHome := filepath.Join(sessionDir, "config")
	launchersDir := filepath.Join(configHome, "plank", dockName, "launchers")
	keyfilePath := filepath.Join(configHome, gsettingsKeyfileRelativePath)

	if err := os.RemoveAll(launchersDir); err != nil {
		return plankConfig{}, fmt.Errorf("reset dock launchers dir: %w", err)
	}
	if err := os.MkdirAll(launchersDir, 0o755); err != nil {
		return plankConfig{}, fmt.Errorf("create dock launchers dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(keyfilePath), 0o755); err != nil {
		return plankConfig{}, fmt.Errorf("create dock settings dir: %w", err)
	}

	launchers := dockLaunchersForApps(apps)
	launcherNames := make([]string, 0, len(launchers))
	for _, launcher := range launchers {
		launcherURL := (&url.URL{Scheme: "file", Path: launcher.DesktopFile}).String()
		contents := fmt.Sprintf("[PlankDockItemPreferences]\nLauncher=%s\n", launcherURL)
		launcherPath := filepath.Join(launchersDir, launcher.DockItemName)
		if err := os.WriteFile(launcherPath, []byte(contents), 0o644); err != nil {
			return plankConfig{}, fmt.Errorf("write dock launcher %q: %w", launcher.DockItemName, err)
		}
		launcherNames = append(launcherNames, launcher.DockItemName)
	}

	if err := os.WriteFile(keyfilePath, []byte(renderPlankKeyfile(launcherNames)), 0o644); err != nil {
		return plankConfig{}, fmt.Errorf("write dock settings keyfile: %w", err)
	}

	return plankConfig{
		DockName:      dockName,
		ConfigHome:    configHome,
		LauncherNames: launcherNames,
	}, nil
}

// StartDock starts the packaged Plank dock using the session-local config and
// patched gdk-pixbuf cache.
func StartDock(runtimeDir string, display int, sessionDir string, detached bool) (int, error) {
	apps, err := FindApplications()
	if err != nil {
		return 0, fmt.Errorf("find applications: %w", err)
	}

	config, err := SetupPlankConfig(sessionDir, apps)
	if err != nil {
		return 0, fmt.Errorf("setup dock config: %w", err)
	}

	pixbufCache, err := PatchGdkPixbufCache(runtimeDir, sessionDir)
	if err != nil {
		return 0, fmt.Errorf("patch gdk-pixbuf cache: %w", err)
	}

	for _, dir := range []string{
		filepath.Join(sessionDir, "cache"),
		filepath.Join(sessionDir, "data"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return 0, fmt.Errorf("create dock runtime dir %q: %w", dir, err)
		}
	}

	plankBin := filepath.Join(runtimeDir, "bin", "plank")
	if _, err := os.Stat(plankBin); err != nil {
		return 0, fmt.Errorf("runtime plank binary missing: %w", err)
	}

	dockLog, err := os.OpenFile(
		filepath.Join(sessionDir, dockLogFileName),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY,
		0o644,
	)
	if err != nil {
		return 0, fmt.Errorf("open dock log: %w", err)
	}
	defer dockLog.Close()

	if _, err := fmt.Fprintf(
		dockLog,
		"[%s] starting dock with %d launcher(s)\n",
		time.Now().UTC().Format(time.RFC3339),
		len(config.LauncherNames),
	); err != nil {
		return 0, fmt.Errorf("write dock log preamble: %w", err)
	}

	dockCmd := exec.Command(runtime.ResolveRuntimeBinary(runtimeDir, "plank"), "-n", config.DockName)
	dockCmd.Stdout = dockLog
	dockCmd.Stderr = dockLog
	dockCmd.Env = BuildEnv(runtimeDir, display, map[string]string{
		"XDG_CONFIG_HOME":        config.ConfigHome,
		"XDG_CACHE_HOME":         filepath.Join(sessionDir, "cache"),
		"XDG_DATA_HOME":          filepath.Join(sessionDir, "data"),
		"GSETTINGS_BACKEND":      "keyfile",
		"GDK_PIXBUF_MODULE_FILE": pixbufCache,
	})
	if detached {
		dockCmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	}

	if err := dockCmd.Start(); err != nil {
		return 0, fmt.Errorf("start plank: %w", err)
	}

	waitErrCh := make(chan error, 1)
	go func() {
		waitErrCh <- dockCmd.Wait()
	}()

	select {
	case err := <-waitErrCh:
		if err != nil {
			return 0, fmt.Errorf("plank exited during startup: %w", err)
		}
		return 0, fmt.Errorf("plank exited during startup")
	case <-time.After(dockStartupGracePeriod):
	}

	return dockCmd.Process.Pid, nil
}

func appendDockLog(sessionDir, format string, args ...any) error {
	dockLog, err := os.OpenFile(
		filepath.Join(sessionDir, dockLogFileName),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY,
		0o644,
	)
	if err != nil {
		return err
	}
	defer dockLog.Close()

	_, err = fmt.Fprintf(
		dockLog,
		"[%s] %s\n",
		time.Now().UTC().Format(time.RFC3339),
		fmt.Sprintf(format, args...),
	)
	return err
}

func dockLaunchersForApps(apps []DesktopApp) []dockLauncherSpec {
	launchers := make([]dockLauncherSpec, 0, len(apps))
	for _, app := range apps {
		if app.DesktopFile == "" {
			continue
		}
		dockItemName := strings.TrimSuffix(filepath.Base(app.DesktopID), ".desktop") + ".dockitem"
		if dockItemName == ".dockitem" {
			dockItemName = strings.TrimSuffix(filepath.Base(app.DesktopFile), ".desktop") + ".dockitem"
		}
		launchers = append(launchers, dockLauncherSpec{
			DockItemName: dockItemName,
			DesktopFile:  app.DesktopFile,
		})
	}
	return launchers
}

func findRuntimeGdkPixbufCache(runtimeDir string) (string, error) {
	root := filepath.Join(runtimeDir, "lib", "gdk-pixbuf-2.0")
	var matches []string
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() == "loaders.cache" {
			matches = append(matches, path)
		}
		return nil
	}); err != nil {
		return "", fmt.Errorf("walk runtime gdk-pixbuf cache: %w", err)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("runtime gdk-pixbuf loaders.cache not found under %q", root)
	}

	sort.Strings(matches)
	return matches[0], nil
}

func renderPlankKeyfile(launcherNames []string) string {
	var b strings.Builder
	b.WriteString("[net/launchpad/plank]\n")
	b.WriteString("enabled-docks=['dock1']\n\n")
	b.WriteString("[net/launchpad/plank/docks/dock1]\n")
	b.WriteString("alignment='center'\n")
	b.WriteString("current-workspace-only=false\n")
	b.WriteString("dock-items=")
	b.WriteString(renderGSettingsStringArray(launcherNames))
	b.WriteString("\n")
	b.WriteString("gap-size=0\n")
	b.WriteString("hide-mode='none'\n")
	b.WriteString("icon-size=48\n")
	b.WriteString("lock-items=true\n")
	b.WriteString("pinned-only=true\n")
	b.WriteString("position='bottom'\n")
	b.WriteString("theme='Default'\n")
	b.WriteString("zoom-enabled=false\n")
	b.WriteString("zoom-percent=150\n")
	return b.String()
}

func renderGSettingsStringArray(values []string) string {
	if len(values) == 0 {
		return "[]"
	}

	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, fmt.Sprintf("'%s'", strings.ReplaceAll(value, "'", "\\'")))
	}
	return fmt.Sprintf("[%s]", strings.Join(quoted, ", "))
}
