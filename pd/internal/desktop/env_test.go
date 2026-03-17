package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildEnv_PathPrepend(t *testing.T) {
	env := BuildEnv("/opt/runtime", 1, nil)
	m := envMap(env)

	if !strings.HasPrefix(m["PATH"], "/opt/runtime/bin:") {
		t.Fatalf("expected PATH to start with /opt/runtime/bin:, got %s", m["PATH"])
	}
}

func TestBuildEnv_Display(t *testing.T) {
	env := BuildEnv("/opt/runtime", 42, nil)
	m := envMap(env)

	if m["DISPLAY"] != ":42" {
		t.Fatalf("expected DISPLAY=:42, got %s", m["DISPLAY"])
	}
}

func TestBuildEnv_LangFallback(t *testing.T) {
	// Unset LANG for this test.
	orig := os.Getenv("LANG")
	os.Unsetenv("LANG")
	defer func() {
		if orig != "" {
			os.Setenv("LANG", orig)
		}
	}()

	env := BuildEnv("/opt/runtime", 1, nil)
	m := envMap(env)

	if m["LANG"] != "C.UTF-8" {
		t.Fatalf("expected LANG=C.UTF-8 when unset, got %s", m["LANG"])
	}
}

func TestBuildEnv_LangPreservedIfUtf8(t *testing.T) {
	orig := os.Getenv("LANG")
	os.Setenv("LANG", "en_US.UTF-8")
	defer func() {
		if orig != "" {
			os.Setenv("LANG", orig)
		} else {
			os.Unsetenv("LANG")
		}
	}()

	env := BuildEnv("/opt/runtime", 1, nil)
	m := envMap(env)

	if m["LANG"] != "en_US.UTF-8" {
		t.Fatalf("expected LANG=en_US.UTF-8, got %s", m["LANG"])
	}
}

func TestBuildEnv_XDGDataDirsFallback(t *testing.T) {
	orig := os.Getenv("XDG_DATA_DIRS")
	os.Unsetenv("XDG_DATA_DIRS")
	defer func() {
		if orig != "" {
			os.Setenv("XDG_DATA_DIRS", orig)
		} else {
			os.Unsetenv("XDG_DATA_DIRS")
		}
	}()

	runtimeDir := "/opt/runtime"
	env := BuildEnv(runtimeDir, 1, nil)
	m := envMap(env)
	want := strings.Join([]string{"/usr/local/share", "/usr/share", filepath.Join(runtimeDir, "share")}, ":")
	if m["XDG_DATA_DIRS"] != want {
		t.Fatalf("expected XDG_DATA_DIRS=%q, got %q", want, m["XDG_DATA_DIRS"])
	}
}

func TestBuildEnv_XDGDataDirsAppendsRuntimeShare(t *testing.T) {
	orig := os.Getenv("XDG_DATA_DIRS")
	os.Setenv("XDG_DATA_DIRS", "/custom/share:/usr/share")
	defer func() {
		if orig != "" {
			os.Setenv("XDG_DATA_DIRS", orig)
		} else {
			os.Unsetenv("XDG_DATA_DIRS")
		}
	}()

	runtimeDir := "/opt/runtime"
	env := BuildEnv(runtimeDir, 1, nil)
	m := envMap(env)
	want := strings.Join([]string{"/custom/share", "/usr/share", filepath.Join(runtimeDir, "share")}, ":")
	if m["XDG_DATA_DIRS"] != want {
		t.Fatalf("expected XDG_DATA_DIRS=%q, got %q", want, m["XDG_DATA_DIRS"])
	}
}

func TestBuildEnv_XDGDataDirsDeduplicatesRuntimeShare(t *testing.T) {
	orig := os.Getenv("XDG_DATA_DIRS")
	runtimeDir := "/opt/runtime"
	runtimeShare := filepath.Join(runtimeDir, "share")
	os.Setenv("XDG_DATA_DIRS", strings.Join([]string{"/usr/share", runtimeShare, "/usr/share"}, ":"))
	defer func() {
		if orig != "" {
			os.Setenv("XDG_DATA_DIRS", orig)
		} else {
			os.Unsetenv("XDG_DATA_DIRS")
		}
	}()

	env := BuildEnv(runtimeDir, 1, nil)
	m := envMap(env)
	want := strings.Join([]string{"/usr/share", runtimeShare}, ":")
	if m["XDG_DATA_DIRS"] != want {
		t.Fatalf("expected XDG_DATA_DIRS=%q, got %q", want, m["XDG_DATA_DIRS"])
	}
}
