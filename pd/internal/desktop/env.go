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
// DISPLAY, ensures LANG and LC_CTYPE contain a UTF-8 locale, ensures
// XDG_DATA_DIRS includes the runtime share directory after host data
// directories, and applies any extra overrides.
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
	env["XDG_DATA_DIRS"] = buildXDGDataDirs(runtimeDir, env["XDG_DATA_DIRS"])

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

func buildXDGDataDirs(runtimeDir, current string) string {
	runtimeShare := filepath.Join(runtimeDir, "share")
	if strings.TrimSpace(current) == "" {
		return strings.Join([]string{"/usr/local/share", "/usr/share", runtimeShare}, ":")
	}

	parts := splitPathList(current)
	parts = append(parts, runtimeShare)
	return strings.Join(uniqueStrings(parts), ":")
}

func splitPathList(value string) []string {
	parts := strings.Split(value, string(os.PathListSeparator))
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

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
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
