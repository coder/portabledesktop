package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// BuildEnv builds an environment variable slice for child processes.
// It starts from os.Environ(), prepends runtimeDir/bin to PATH, sets
// DISPLAY, ensures LANG and LC_CTYPE contain a UTF-8 locale, and
// applies any extra overrides.
func BuildEnv(runtimeDir string, display int, extraEnv map[string]string) []string {
	env := envMap(os.Environ())

	// Prepend runtimeDir/bin to PATH.
	runtimeBin := filepath.Join(runtimeDir, "bin")
	if cur, ok := env["PATH"]; ok && cur != "" {
		env["PATH"] = runtimeBin + ":" + cur
	} else {
		env["PATH"] = runtimeBin
	}

	env["DISPLAY"] = fmt.Sprintf(":%d", display)

	// Ensure a UTF-8 LANG.
	langVal := env["LANG"]
	if langVal == "" || !containsUTF8(langVal) {
		env["LANG"] = "C.UTF-8"
	}

	// Ensure a UTF-8 LC_CTYPE, defaulting to whatever LANG is now.
	lcVal := env["LC_CTYPE"]
	if lcVal == "" || !containsUTF8(lcVal) {
		env["LC_CTYPE"] = env["LANG"]
	}

	// Apply caller-supplied overrides last.
	for k, v := range extraEnv {
		env[k] = v
	}

	return envSlice(env)
}

// containsUTF8 reports whether s contains "utf-8" or "utf8"
// (case-insensitive).
func containsUTF8(s string) bool {
	low := strings.ToLower(s)
	return strings.Contains(low, "utf-8") || strings.Contains(low, "utf8")
}

// envMap converts a []string of KEY=VALUE pairs into a map. Later
// entries for the same key win.
func envMap(environ []string) map[string]string {
	m := make(map[string]string, len(environ))
	for _, entry := range environ {
		k, v, ok := strings.Cut(entry, "=")
		if ok {
			m[k] = v
		}
	}
	return m
}

// envSlice converts a map back to a sorted KEY=VALUE slice.
func envSlice(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}
