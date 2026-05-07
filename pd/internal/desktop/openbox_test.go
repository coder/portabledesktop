package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupOpenboxConfigStripsRightClickMousebind(t *testing.T) {
	t.Parallel()

	runtimeDir := t.TempDir()
	xdgDir := filepath.Join(runtimeDir, "etc", "xdg", "openbox")
	if err := os.MkdirAll(xdgDir, 0o755); err != nil {
		t.Fatalf("mkdir xdg dir: %v", err)
	}

	// Use a fragment that mirrors the runtime rc.xml structure
	// closely enough to exercise the stripping regex against the
	// real `<context name="Root">` / `<mousebind ...>` shape.
	rc := `<?xml version="1.0" encoding="UTF-8"?>
<openbox_config xmlns="http://openbox.org/3.4/rc">
  <mouse>
    <context name="Root">
      <mousebind button="Middle" action="Press">
        <action name="ShowMenu"><menu>client-list-combined-menu</menu></action>
      </mousebind>
      <mousebind button="Right" action="Press">
        <action name="ShowMenu"><menu>root-menu</menu></action>
      </mousebind>
    </context>
  </mouse>
</openbox_config>
`
	if err := os.WriteFile(filepath.Join(xdgDir, "rc.xml"), []byte(rc), 0o644); err != nil {
		t.Fatalf("write rc.xml: %v", err)
	}

	sessionDir := t.TempDir()
	configHome, err := SetupOpenboxConfig(runtimeDir, sessionDir)
	if err != nil {
		t.Fatalf("SetupOpenboxConfig() error = %v", err)
	}
	if configHome == "" {
		t.Fatal("SetupOpenboxConfig() returned empty configHome despite runtime rc.xml present")
	}

	rcOut, err := os.ReadFile(filepath.Join(configHome, "openbox", "rc.xml"))
	if err != nil {
		t.Fatalf("read patched rc.xml: %v", err)
	}
	got := string(rcOut)
	if strings.Contains(got, `button="Right"`) {
		t.Fatalf("patched rc.xml still contains right-click mousebind:\n%s", got)
	}
	// Middle-click binding must remain.
	if !strings.Contains(got, `button="Middle"`) {
		t.Fatalf("patched rc.xml unexpectedly removed middle-click mousebind:\n%s", got)
	}
	// Surrounding `<context name="Root">` block must remain so the
	// remaining mousebinds keep their context.
	if !strings.Contains(got, `<context name="Root">`) {
		t.Fatalf("patched rc.xml lost <context name=\"Root\">:\n%s", got)
	}

	menuOut, err := os.ReadFile(filepath.Join(configHome, "openbox", "menu.xml"))
	if err != nil {
		t.Fatalf("read session menu.xml: %v", err)
	}
	if !strings.Contains(string(menuOut), `id="root-menu"`) {
		t.Fatalf("session menu.xml missing root-menu definition:\n%s", menuOut)
	}
}

func TestSetupOpenboxConfigMissingRuntimeReturnsEmpty(t *testing.T) {
	t.Parallel()

	runtimeDir := t.TempDir() // no etc/xdg/openbox/rc.xml inside.
	sessionDir := t.TempDir()

	configHome, err := SetupOpenboxConfig(runtimeDir, sessionDir)
	if err != nil {
		t.Fatalf("SetupOpenboxConfig() error = %v", err)
	}
	if configHome != "" {
		t.Fatalf("SetupOpenboxConfig() configHome = %q, want empty when runtime lacks rc.xml", configHome)
	}
}
