// Package openaiagent implements the computer-use Dispatcher for
// OpenAI's Responses API computer tool.
//
// The dispatcher follows the guidance in OpenAI's computer-use guide:
//
//   - Run every action in actions[] in order, capture a screenshot, and
//     return it as the tool result. Per-action errors are logged and the
//     batch continues so the model still sees the resulting screen state.
//   - Honor the optional `keys` modifier array on click, double_click,
//     drag, move, and scroll. Modifiers are pressed before the mouse
//     gesture and released after.
//   - Normalize model-emitted key names (CTRL, META, ARROWLEFT, ...)
//     onto the names the desktop keyboard CLI recognises.
package openaiagent

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"strconv"
	"strings"
	"time"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/openai"
	openaisdk "github.com/charmbracelet/openai-go/responses"

	"github.com/coder/portabledesktop/examples/agent-go/computeruse"
	"github.com/coder/portabledesktop/examples/agent-go/desktop"
	"github.com/coder/portabledesktop/examples/agent-go/reporting"
)

// Dispatcher implements computeruse.Dispatcher for OpenAI.
type Dispatcher struct {
	cfg  *computeruse.Config
	tool fantasy.Tool
}

// New creates a Dispatcher configured for OpenAI computer use.
func New(cfg *computeruse.Config) *Dispatcher {
	tool := openai.NewComputerUseTool(
		// The manual agent loop drives execution directly. This
		// callback is only invoked if a caller chooses to use
		// fantasy's built-in agent helper, which we do not.
		func(_ context.Context, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.ToolResponse{}, fmt.Errorf("openai computer use tool callback is unused; the example drives execution from runAgentLoop")
		},
	)
	return &Dispatcher{cfg: cfg, tool: tool}
}

// Tool implements computeruse.Dispatcher.
func (d *Dispatcher) Tool() fantasy.Tool {
	return d.tool
}

// Execute runs every action in a single OpenAI computer_call and
// returns one tool result containing the resulting screenshot. OpenAI
// batches multiple actions per call, but the wire format only allows
// a single image_url back, so we always end the batch with one
// screenshot. Per-action errors are logged to stderr (because the
// computer_call_output schema accepts image data only) and the batch
// continues so the model can see actual desktop state in the returned
// screenshot.
//
// The returned tool result carries OpenAI's ComputerCallOutputOptions
// with Detail set to "original" so the screenshot is forwarded at full
// resolution, matching the recommendation in OpenAI's computer use
// guide.
func (d *Dispatcher) Execute(
	_ context.Context,
	tc fantasy.ToolCallContent,
	metrics *reporting.AgentMetrics,
) (computeruse.ExecuteResult, string, error) {
	parsed, err := openai.ParseComputerUseInput(tc.Input)
	if err != nil {
		return computeruse.ExecuteResult{
			Output: fantasy.ToolResultOutputContentText{Text: fmt.Sprintf("invalid input: %v", err)},
		}, "<unparsed>", nil
	}

	labels := make([]string, 0, len(parsed.Actions))
	for i, action := range parsed.Actions {
		labels = append(labels, formatAction(action))

		if err := d.executeAction(action, metrics); err != nil {
			// Log and continue. The OpenAI guide tells us to run
			// every action in actions[] in order, and the
			// computer_call_output schema only accepts image data,
			// so we cannot signal per-action errors back to the
			// model directly. The screenshot will reflect any side
			// effects of the actions that did succeed.
			fmt.Fprintf(os.Stderr, "    openai action %d (%s) failed: %v\n", i, action.Type, err)
		}
	}

	// sleep for a second before we take the screenshot to allow the desktop to settle
	time.Sleep(1 * time.Second)

	// computer_call_output only accepts an image URL, so always
	// capture a fresh screenshot regardless of what the batch did.
	shot, err := computeruse.CaptureScreenshotResult(d.cfg, metrics, nil)
	if err != nil {
		return computeruse.ExecuteResult{}, joinLabels(labels), err
	}
	if len(shot) == 0 {
		return computeruse.ExecuteResult{
			Output: fantasy.ToolResultOutputContentText{Text: "no result"},
		}, joinLabels(labels), nil
	}
	return computeruse.ExecuteResult{
		Output: shot[0],
		ProviderOptions: fantasy.ProviderOptions{
			openai.Name: &openai.ComputerCallOutputOptions{Detail: "original"},
		},
	}, joinLabels(labels), nil
}

// executeAction performs a single action against the portabledesktop
// CLI. It returns an error only when the action cannot be executed
// (bad button, missing path, CLI failure). The caller is responsible
// for screenshot capture.
func (d *Dispatcher) executeAction(
	action openaisdk.ComputerActionUnion,
	metrics *reporting.AgentMetrics,
) error {
	move := func(label string, point image.Point) error {
		return computeruse.MoveMouseToDeclaredPoint(d.cfg, label, point, metrics)
	}

	// Modifier keys are exposed as a top-level field on
	// ComputerActionUnion; the SDK populates it from the click,
	// double_click, drag, move, and scroll variants when present.
	holdModifiers := func(fn func() error) error {
		return withHeldModifiers(action.Keys, fn)
	}

	switch action.Type {
	case "screenshot":
		// Screenshots are captured once per batch by the caller.
		return nil

	case "click":
		click := action.AsClick()
		button, err := mapClickButton(click.Button)
		if err != nil {
			return err
		}
		return holdModifiers(func() error {
			if err := move("click", image.Pt(int(click.X), int(click.Y))); err != nil {
				return err
			}
			if err := desktop.ExecVoid("mouse", "click", button); err != nil {
				return fmt.Errorf("mouse click %s: %w", button, err)
			}
			if metrics != nil {
				metrics.Clicks++
			}
			return nil
		})

	case "double_click":
		dc := action.AsDoubleClick()
		return holdModifiers(func() error {
			if err := move("double_click", image.Pt(int(dc.X), int(dc.Y))); err != nil {
				return err
			}
			for i := 0; i < 2; i++ {
				if err := desktop.ExecVoid("mouse", "click", "left"); err != nil {
					return fmt.Errorf("mouse click left (double_click %d): %w", i, err)
				}
				if metrics != nil {
					metrics.Clicks++
				}
			}
			return nil
		})

	case "move":
		mv := action.AsMove()
		return holdModifiers(func() error {
			return move("move", image.Pt(int(mv.X), int(mv.Y)))
		})

	case "scroll":
		sc := action.AsScroll()
		return holdModifiers(func() error {
			if err := move("scroll", image.Pt(int(sc.X), int(sc.Y))); err != nil {
				return err
			}
			if err := desktop.ExecVoid(
				"mouse", "scroll",
				strconv.Itoa(int(sc.ScrollX)),
				strconv.Itoa(int(sc.ScrollY)),
			); err != nil {
				return fmt.Errorf("mouse scroll: %w", err)
			}
			return nil
		})

	case "type":
		ty := action.AsType()
		if ty.Text == "" {
			return fmt.Errorf("text is required for type action")
		}
		if err := desktop.ExecVoid("keyboard", "type", ty.Text); err != nil {
			return fmt.Errorf("keyboard type: %w", err)
		}
		return nil

	case "keypress":
		kp := action.AsKeypress()
		if len(kp.Keys) == 0 {
			return fmt.Errorf("keys is required for keypress action")
		}
		// The keyboard CLI expects '+' as the modifier separator
		// (e.g. "ctrl+s"). Normalize each key first because OpenAI
		// emits names like CTRL, META, and ARROWLEFT that xdotool
		// does not recognise.
		combo := strings.Join(normalizeKeys(kp.Keys), "+")
		if err := desktop.ExecVoid("keyboard", "key", combo); err != nil {
			return fmt.Errorf("keyboard key %q: %w", combo, err)
		}
		return nil

	case "drag":
		dr := action.AsDrag()
		if len(dr.Path) < 2 {
			return fmt.Errorf("drag requires at least two path points")
		}
		return holdModifiers(func() error {
			first := image.Pt(int(dr.Path[0].X), int(dr.Path[0].Y))
			if err := move("drag.start", first); err != nil {
				return err
			}
			if err := desktop.ExecVoid("mouse", "down", "left"); err != nil {
				return fmt.Errorf("mouse down left (drag): %w", err)
			}
			// Move along the path so apps that distinguish drags
			// from teleports observe a continuous gesture.
			for i, p := range dr.Path[1:] {
				label := fmt.Sprintf("drag.path[%d]", i+1)
				if err := move(label, image.Pt(int(p.X), int(p.Y))); err != nil {
					_ = desktop.ExecVoid("mouse", "up", "left")
					return err
				}
			}
			if err := desktop.ExecVoid("mouse", "up", "left"); err != nil {
				return fmt.Errorf("mouse up left (drag): %w", err)
			}
			return nil
		})

	case "wait":
		// OpenAI's wait action carries no duration. Use a short
		// pause that still gives the UI a moment to settle.
		if metrics != nil {
			metrics.Waits++
		}
		time.Sleep(1 * time.Second)
		return nil

	default:
		return fmt.Errorf("unsupported openai action: %s", action.Type)
	}
}

// withHeldModifiers presses every key in keys before invoking fn and
// releases them after, mirroring xdotool's keydown/keyup pattern. If
// any key fails to press, the previously-pressed keys are released
// before returning.
func withHeldModifiers(keys []string, fn func() error) error {
	if len(keys) == 0 {
		return fn()
	}
	pressed := make([]string, 0, len(keys))
	release := func() {
		for i := len(pressed) - 1; i >= 0; i-- {
			_ = desktop.ExecVoid("keyboard", "up", pressed[i])
		}
	}
	for _, k := range keys {
		normalized := normalizeKey(k)
		if err := desktop.ExecVoid("keyboard", "down", normalized); err != nil {
			release()
			return fmt.Errorf("hold modifier %q: %w", k, err)
		}
		pressed = append(pressed, normalized)
	}
	defer release()
	return fn()
}

// keyNameAliases maps model-emitted key names that the desktop
// keyboard CLI does not natively recognise onto names xdotool
// understands. The CLI already lowercases common modifiers, so this
// table focuses on the symbolic names the OpenAI guide flags
// explicitly (ARROWLEFT, etc.).
var keyNameAliases = map[string]string{
	"arrowleft":  "Left",
	"arrowright": "Right",
	"arrowup":    "Up",
	"arrowdown":  "Down",
	"esc":        "Escape",
	"escape":     "Escape",
}

// normalizeKey rewrites model-emitted key names onto names the
// keyboard CLI recognises. Names that are already valid pass through
// unchanged.
func normalizeKey(key string) string {
	if mapped, ok := keyNameAliases[strings.ToLower(key)]; ok {
		return mapped
	}
	return key
}

func normalizeKeys(keys []string) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = normalizeKey(k)
	}
	return out
}

// mapClickButton maps an OpenAI click button onto the buttons the
// portabledesktop mouse CLI accepts. The CLI does not expose back or
// forward, so they are reported as errors rather than silently
// re-mapped to a left click.
func mapClickButton(button string) (string, error) {
	switch button {
	case "left", "right":
		return button, nil
	case "wheel":
		// Wheel clicks are middle clicks on every desktop the CLI
		// supports.
		return "middle", nil
	case "back", "forward":
		return "", fmt.Errorf("click button %q is not supported by the portabledesktop mouse CLI", button)
	default:
		return "", fmt.Errorf("unknown click button %q", button)
	}
}

// formatAction renders the action as a compact JSON snippet, using
// the SDK's raw payload when available.
func formatAction(action openaisdk.ComputerActionUnion) string {
	if raw := action.RawJSON(); raw != "" {
		return raw
	}
	data, err := json.Marshal(action)
	if err != nil {
		return fmt.Sprintf("{\"type\":%q}", action.Type)
	}
	return string(data)
}

func joinLabels(labels []string) string {
	if len(labels) == 0 {
		return ""
	}
	if len(labels) == 1 {
		return labels[0]
	}
	return "[" + strings.Join(labels, ", ") + "]"
}
