package desktop

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// MoveMouse moves the mouse pointer to the given absolute
// coordinates.
func (d *Desktop) MoveMouse(x, y int) error {
	// we don't use --sync because it very often times out
	// even though the mouse moved
	return d.runTool("xdotool", []string{
		"mousemove", strconv.Itoa(x), strconv.Itoa(y),
	})
}

// MousePosition returns the current mouse pointer coordinates.
func (d *Desktop) MousePosition() (x, y int, err error) {
	out, err := d.runToolCapture("xdotool", []string{"getmouselocation", "--shell"})
	if err != nil {
		return 0, 0, err
	}

	xRe := regexp.MustCompile(`X=(\d+)`)
	yRe := regexp.MustCompile(`Y=(\d+)`)
	xMatch := xRe.FindStringSubmatch(out)
	yMatch := yRe.FindStringSubmatch(out)
	if xMatch == nil || yMatch == nil {
		return 0, 0, fmt.Errorf("failed to parse cursor position from xdotool output: %s", out)
	}

	px, _ := strconv.Atoi(xMatch[1])
	py, _ := strconv.Atoi(yMatch[1])
	return px, py, nil
}

// Click performs a mouse click with the given button name or number.
func (d *Desktop) Click(button string) error {
	return d.runTool("xdotool", []string{
		"click", strconv.Itoa(buttonToNumber(button)),
	})
}

// MouseDown presses the given mouse button without releasing it.
func (d *Desktop) MouseDown(button string) error {
	return d.runTool("xdotool", []string{
		"mousedown", strconv.Itoa(buttonToNumber(button)),
	})
}

// MouseUp releases the given mouse button.
func (d *Desktop) MouseUp(button string) error {
	return d.runTool("xdotool", []string{
		"mouseup", strconv.Itoa(buttonToNumber(button)),
	})
}

// Scroll performs scroll events using xdotool click with X11 button
// numbers: 4=scroll-up, 5=scroll-down, 6=scroll-left, 7=scroll-right.
func (d *Desktop) Scroll(dx, dy int) error {
	type scrollEntry struct {
		count  int
		button string
	}

	entries := []scrollEntry{
		{count: int(math.Abs(float64(dy))), button: scrollButton(dy, true)},
		{count: int(math.Abs(float64(dx))), button: scrollButton(dx, false)},
	}

	for _, e := range entries {
		if e.button == "" {
			continue
		}
		for i := 0; i < e.count; i++ {
			if err := d.runTool("xdotool", []string{"click", e.button}); err != nil {
				return err
			}
		}
	}
	return nil
}

// scrollButton returns the X11 button number string for a scroll
// axis. For the vertical axis (isY=true): negative→"4" (up),
// positive→"5" (down). For horizontal: negative→"6" (left),
// positive→"7" (right). Returns "" when delta is zero.
func scrollButton(delta int, isY bool) string {
	if delta == 0 {
		return ""
	}
	if isY {
		if delta < 0 {
			return "4"
		}
		return "5"
	}
	if delta < 0 {
		return "6"
	}
	return "7"
}

// Type types the given text string with a 1ms inter-key delay.
func (d *Desktop) Type(text string) error {
	return d.runTool("xdotool", []string{"type", "--delay", "1", "--", text})
}

// Key sends a key combination, clearing any active modifiers first.
// The combo is normalized so that LLM-style names like "CTRL+L" are
// translated to xdotool keysyms like "ctrl+l".
func (d *Desktop) Key(combo string) error {
	return d.runTool("xdotool", []string{"key", "--clearmodifiers", normalizeCombo(combo)})
}

// KeyDown presses a key without releasing it.
func (d *Desktop) KeyDown(key string) error {
	return d.runTool("xdotool", []string{"keydown", normalizeKeyName(key)})
}

// KeyUp releases a previously pressed key.
func (d *Desktop) KeyUp(key string) error {
	return d.runTool("xdotool", []string{"keyup", normalizeKeyName(key)})
}

// keyNameMap maps case-folded key names that LLMs commonly produce to
// the X11 keysym strings that xdotool expects. Entries are keyed by
// the lowercase form so lookups can be case-insensitive.
var keyNameMap = map[string]string{
	// Modifiers.
	"ctrl":    "ctrl",
	"control": "ctrl",
	"alt":     "alt",
	"shift":   "shift",
	"super":   "super",
	"meta":    "super",
	"command": "super",
	"cmd":     "super",

	// Common special keys.
	"return":    "Return",
	"enter":     "Return",
	"backspace": "BackSpace",
	"tab":       "Tab",
	"escape":    "Escape",
	"esc":       "Escape",
	"space":     "space",
	"delete":    "Delete",
	"del":       "Delete",
	"insert":    "Insert",
	"home":      "Home",
	"end":       "End",
	"pageup":    "Prior",
	"page_up":   "Prior",
	"pagedown":  "Next",
	"page_down": "Next",

	// Arrow keys.
	"up":    "Up",
	"down":  "Down",
	"left":  "Left",
	"right": "Right",

	// Function keys.
	"f1":  "F1",
	"f2":  "F2",
	"f3":  "F3",
	"f4":  "F4",
	"f5":  "F5",
	"f6":  "F6",
	"f7":  "F7",
	"f8":  "F8",
	"f9":  "F9",
	"f10": "F10",
	"f11": "F11",
	"f12": "F12",
}

// normalizeKeyName translates a single key name into the X11 keysym
// that xdotool expects. LLMs frequently send names like "CTRL",
// "ENTER", or "F4" in varying cases that xdotool does not recognise.
// Known names are resolved through keyNameMap. Single ASCII letters
// are lowercased because xdotool treats an uppercase letter as
// shift+<letter>. Anything else passes through unchanged so that
// already-valid keysyms are not broken.
func normalizeKeyName(name string) string {
	if mapped, ok := keyNameMap[strings.ToLower(name)]; ok {
		return mapped
	}
	// A bare uppercase ASCII letter should be lowered; xdotool
	// interprets "L" as shift+l which is not the intended behaviour
	// when an LLM sends "CTRL+L".
	if len(name) == 1 && name[0] >= 'A' && name[0] <= 'Z' {
		return strings.ToLower(name)
	}
	return name
}

// normalizeCombo normalises a "+"-separated key combination string.
// Each component is passed through normalizeKeyName so that
// "CTRL+L" becomes "ctrl+l" and "ALT+F4" becomes "alt+F4".
func normalizeCombo(combo string) string {
	parts := strings.Split(combo, "+")
	for i, p := range parts {
		parts[i] = normalizeKeyName(p)
	}
	return strings.Join(parts, "+")
}

// buttonToNumber maps a button name to its X11 button number.
// "left"→1, "middle"→2, "right"→3. Numeric strings are parsed
// directly. Unknown values default to 1.
func buttonToNumber(button string) int {
	switch strings.ToLower(button) {
	case "left":
		return 1
	case "middle":
		return 2
	case "right":
		return 3
	default:
		n, err := strconv.Atoi(button)
		if err != nil || n < 1 {
			return 1
		}
		return n
	}
}
