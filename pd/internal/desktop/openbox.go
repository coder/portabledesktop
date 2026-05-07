package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/coder/portabledesktop/pd/internal/runtime"
)

// openboxRightClickMousebindRE matches the entire `<mousebind
// button="Right" action="Press">...</mousebind>` block inside the
// runtime's `<context name="Root">` section. We strip this block to
// disable the desktop right-click menu, which would otherwise show
// host-targeted launchers (gnome-terminal, konsole, firefox, etc.)
// that do not exist inside the portabledesktop runtime.
var openboxRightClickMousebindRE = regexp.MustCompile(
	`(?s)\s*<mousebind\s+button="Right"\s+action="Press">.*?</mousebind>`,
)

// emptyOpenboxMenuXML is a minimal menu file that defines an empty
// `root-menu`. We write it to the session config dir so openbox does
// not log "menu not found" warnings if any other code path still
// references the root menu after the right-click binding is stripped.
const emptyOpenboxMenuXML = `<?xml version="1.0" encoding="UTF-8"?>
<openbox_menu xmlns="http://openbox.org/3.4/menu">
  <menu id="root-menu" label="Coder">
    <separator label="Coder"/>
  </menu>
</openbox_menu>
`

// SetupOpenboxConfig writes a session-local openbox config tree
// derived from the runtime's `etc/xdg/openbox/rc.xml`, with the
// desktop right-click `ShowMenu` mousebind stripped out and a
// minimal `menu.xml` written alongside it. The returned path is
// suitable for use as `XDG_CONFIG_HOME` when launching openbox: the
// runtime's own configs remain intact via `XDG_CONFIG_DIRS`, but
// openbox prefers `$XDG_CONFIG_HOME/openbox/rc.xml` when present.
//
// If the runtime does not ship a system rc.xml (for example in
// tests or stripped runtimes), SetupOpenboxConfig is a no-op and
// returns "" with a nil error so the caller can fall back to the
// runtime defaults.
func SetupOpenboxConfig(runtimeDir, sessionDir string) (string, error) {
	src := filepath.Join(runtimeDir, "etc", "xdg", "openbox", "rc.xml")
	contents, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read runtime openbox rc.xml: %w", err)
	}

	patched := openboxRightClickMousebindRE.ReplaceAllString(string(contents), "")

	configHome := filepath.Join(sessionDir, "openbox-config")
	openboxDir := filepath.Join(configHome, "openbox")
	if err := os.MkdirAll(openboxDir, 0o755); err != nil {
		return "", fmt.Errorf("create session openbox config dir: %w", err)
	}

	rcPath := filepath.Join(openboxDir, "rc.xml")
	if err := os.WriteFile(rcPath, []byte(patched), 0o644); err != nil {
		return "", fmt.Errorf("write session openbox rc.xml: %w", err)
	}

	menuPath := filepath.Join(openboxDir, "menu.xml")
	if err := os.WriteFile(menuPath, []byte(emptyOpenboxMenuXML), 0o644); err != nil {
		return "", fmt.Errorf("write session openbox menu.xml: %w", err)
	}

	return configHome, nil
}

// resolveOpenboxRuntimeBinary is a thin wrapper around
// runtime.ResolveRuntimeBinary that exists so unit tests in this
// package can swap out the lookup if needed.
func resolveOpenboxRuntimeBinary(runtimeDir string) string {
	return runtime.ResolveRuntimeBinary(runtimeDir, "openbox")
}
