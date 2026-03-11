package desktop

import (
	"testing"
)

func TestNormalizeKeyName_Modifiers(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"CTRL", "ctrl"},
		{"ctrl", "ctrl"},
		{"Ctrl", "ctrl"},
		{"CONTROL", "ctrl"},
		{"control", "ctrl"},
		{"ALT", "alt"},
		{"alt", "alt"},
		{"SHIFT", "shift"},
		{"shift", "shift"},
		{"SUPER", "super"},
		{"META", "super"},
		{"COMMAND", "super"},
		{"CMD", "super"},
		{"cmd", "super"},
	}
	for _, tc := range cases {
		if got := normalizeKeyName(tc.input); got != tc.want {
			t.Errorf("normalizeKeyName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestNormalizeKeyName_SpecialKeys(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"RETURN", "Return"},
		{"Return", "Return"},
		{"ENTER", "Return"},
		{"enter", "Return"},
		{"BACKSPACE", "BackSpace"},
		{"TAB", "Tab"},
		{"ESCAPE", "Escape"},
		{"ESC", "Escape"},
		{"SPACE", "space"},
		{"DELETE", "Delete"},
		{"DEL", "Delete"},
		{"INSERT", "Insert"},
		{"HOME", "Home"},
		{"END", "End"},
		{"PAGEUP", "Prior"},
		{"PAGE_UP", "Prior"},
		{"PAGEDOWN", "Next"},
		{"PAGE_DOWN", "Next"},
		{"UP", "Up"},
		{"DOWN", "Down"},
		{"LEFT", "Left"},
		{"RIGHT", "Right"},
	}
	for _, tc := range cases {
		if got := normalizeKeyName(tc.input); got != tc.want {
			t.Errorf("normalizeKeyName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestNormalizeKeyName_FunctionKeys(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"f1", "F1"},
		{"F1", "F1"},
		{"f4", "F4"},
		{"F4", "F4"},
		{"f12", "F12"},
		{"F12", "F12"},
	}
	for _, tc := range cases {
		if got := normalizeKeyName(tc.input); got != tc.want {
			t.Errorf("normalizeKeyName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestNormalizeKeyName_SingleLetters(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		// Uppercase single ASCII letters are lowered.
		{"L", "l"},
		{"A", "a"},
		{"Z", "z"},
		// Already-lowercase letters pass through.
		{"l", "l"},
		{"a", "a"},
	}
	for _, tc := range cases {
		if got := normalizeKeyName(tc.input); got != tc.want {
			t.Errorf("normalizeKeyName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestNormalizeKeyName_Passthrough(t *testing.T) {
	// Already-valid X11 keysyms and unknown strings pass through.
	cases := []string{"Return", "BackSpace", "XF86AudioPlay", "1", "semicolon"}
	for _, input := range cases {
		if got := normalizeKeyName(input); got != input {
			t.Errorf("normalizeKeyName(%q) = %q, want passthrough", input, got)
		}
	}
}

func TestNormalizeCombo(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		// Typical LLM-produced combos.
		{"CTRL+L", "ctrl+l"},
		{"ALT+F4", "alt+F4"},
		{"SHIFT+TAB", "shift+Tab"},
		{"CTRL+SHIFT+T", "ctrl+shift+t"},
		{"SUPER+D", "super+d"},
		{"CMD+A", "super+a"},
		// Already-correct xdotool combos are unchanged.
		{"ctrl+l", "ctrl+l"},
		{"alt+F4", "alt+F4"},
		{"shift+Tab", "shift+Tab"},
		// Single key (no +) still normalised.
		{"RETURN", "Return"},
		{"a", "a"},
	}
	for _, tc := range cases {
		if got := normalizeCombo(tc.input); got != tc.want {
			t.Errorf("normalizeCombo(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
