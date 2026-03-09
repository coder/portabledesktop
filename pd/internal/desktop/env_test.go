package desktop

import (
	"os"
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
