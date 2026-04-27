// Package anthropicagent implements the computer-use Dispatcher for
// Anthropic's computer-use tool.
package anthropicagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"

	"github.com/coder/portabledesktop/examples/agent-go/computeruse"
	"github.com/coder/portabledesktop/examples/agent-go/desktop"
	"github.com/coder/portabledesktop/examples/agent-go/reporting"
)

// rawComputerUseInput captures the coordinate / region fields that
// Anthropic's ComputerUseInput leaves untyped. The structured
// ComputerUseInput in the SDK does not expose them as typed fields
// because the same wire payload encodes coordinates differently for
// different actions.
type rawComputerUseInput struct {
	Coordinate      *[2]int64 `json:"coordinate,omitempty"`
	StartCoordinate *[2]int64 `json:"start_coordinate,omitempty"`
	Region          *[4]int64 `json:"region,omitempty"`
}

func parseRawComputerUseInput(input string) (rawComputerUseInput, error) {
	var raw rawComputerUseInput
	if err := json.Unmarshal([]byte(input), &raw); err != nil {
		return rawComputerUseInput{}, err
	}
	return raw, nil
}

// Dispatcher implements computeruse.Dispatcher for Anthropic.
type Dispatcher struct {
	cfg  *computeruse.Config
	tool fantasy.Tool
}

// New creates a Dispatcher configured for Anthropic computer use. The
// declared display dimensions come from cfg.Geometry. EnableZoom is
// always on because the example exposes the zoom action via
// cropped screenshots.
func New(cfg *computeruse.Config) *Dispatcher {
	enableZoom := true
	tool := anthropic.NewComputerUseTool(
		anthropic.ComputerUseToolOptions{
			DisplayWidthPx:  int64(cfg.Geometry.DeclaredWidth),
			DisplayHeightPx: int64(cfg.Geometry.DeclaredHeight),
			EnableZoom:      &enableZoom,
			ToolVersion:     anthropic.ComputerUse20251124,
		},
		// The manual agent loop drives execution directly. This
		// callback is only invoked if a caller chooses to use
		// fantasy's built-in agent helper, which we do not.
		func(_ context.Context, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.ToolResponse{}, fmt.Errorf("anthropic computer use tool callback is unused; the example drives execution from runAgentLoop")
		},
	)
	return &Dispatcher{cfg: cfg, tool: tool}
}

// Tool implements computeruse.Dispatcher.
func (d *Dispatcher) Tool() fantasy.Tool {
	return d.tool
}

// Execute implements computeruse.Dispatcher.
func (d *Dispatcher) Execute(
	_ context.Context,
	tc fantasy.ToolCallContent,
	metrics *reporting.AgentMetrics,
) (computeruse.ExecuteResult, string, error) {
	action, err := anthropic.ParseComputerUseInput(tc.Input)
	if err != nil {
		return computeruse.ExecuteResult{
			Output: fantasy.ToolResultOutputContentText{Text: fmt.Sprintf("invalid input: %v", err)},
		}, "<unparsed>", nil
	}

	label := reporting.FormatAction(action)

	results, err := d.executeAction(action, tc.Input, metrics)
	if err != nil {
		return computeruse.ExecuteResult{}, label, err
	}
	if len(results) == 0 {
		return computeruse.ExecuteResult{
			Output: fantasy.ToolResultOutputContentText{Text: "no result"},
		}, label, nil
	}
	return computeruse.ExecuteResult{Output: results[0]}, label, nil
}

func (d *Dispatcher) executeAction(
	input anthropic.ComputerUseInput,
	rawInput string,
	metrics *reporting.AgentMetrics,
) ([]fantasy.ToolResultOutputContent, error) {
	raw, err := parseRawComputerUseInput(rawInput)
	if err != nil {
		return computeruse.ErrorResult(fmt.Sprintf("parse raw input: %v", err)), nil
	}

	move := func(label string, coordinate [2]int64) error {
		return computeruse.MoveMouseToDeclaredCoordinate(d.cfg, label, coordinate, metrics)
	}
	screenshot := func(region *[4]int) ([]fantasy.ToolResultOutputContent, error) {
		return computeruse.CaptureScreenshotResult(d.cfg, metrics, region)
	}

	switch input.Action {
	case anthropic.ActionKey:
		if input.Text == "" {
			return computeruse.TextResult("text is required for key action"), nil
		}
		if err := desktop.ExecVoid("keyboard", "key", input.Text); err != nil {
			return computeruse.ErrorResult(err.Error()), nil
		}
		return screenshot(nil)

	case anthropic.ActionHoldKey:
		if input.Text == "" {
			return computeruse.ErrorResult("text is required for hold_key action"), nil
		}
		keys := strings.Split(input.Text, "+")
		var pressed []string
		for _, k := range keys {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			if err := desktop.ExecVoid("keyboard", "down", k); err != nil {
				for i := len(pressed) - 1; i >= 0; i-- {
					_ = desktop.ExecVoid("keyboard", "up", pressed[i])
				}
				return computeruse.ErrorResult(err.Error()), nil
			}
			pressed = append(pressed, k)
		}
		dur := 250 * time.Millisecond
		if input.Duration > 0 {
			dur = time.Duration(input.Duration) * time.Second
			if dur < 10*time.Millisecond {
				dur = 10 * time.Millisecond
			}
		}
		time.Sleep(dur)
		for i := len(pressed) - 1; i >= 0; i-- {
			_ = desktop.ExecVoid("keyboard", "up", pressed[i])
		}
		return screenshot(nil)

	case anthropic.ActionType:
		if input.Text == "" {
			return computeruse.ErrorResult("text is required for type action"), nil
		}
		if err := desktop.ExecVoid("keyboard", "type", input.Text); err != nil {
			return computeruse.ErrorResult(err.Error()), nil
		}
		return screenshot(nil)

	case anthropic.ActionMouseMove:
		if raw.Coordinate == nil {
			return computeruse.ErrorResult("coordinate is required for mouse_move action"), nil
		}
		if err := move("mouse_move.coordinate", *raw.Coordinate); err != nil {
			return computeruse.ErrorResult(err.Error()), nil
		}
		return screenshot(nil)

	case anthropic.ActionLeftClick:
		if raw.Coordinate != nil {
			if err := move("left_click.coordinate", *raw.Coordinate); err != nil {
				return computeruse.ErrorResult(err.Error()), nil
			}
		}
		if err := desktop.ExecVoid("mouse", "click", "left"); err != nil {
			return computeruse.ErrorResult(err.Error()), nil
		}
		if metrics != nil {
			metrics.Clicks++
		}
		return screenshot(nil)

	case anthropic.ActionLeftClickDrag:
		if raw.StartCoordinate == nil {
			return computeruse.ErrorResult("start_coordinate is required for left_click_drag action"), nil
		}
		if raw.Coordinate == nil {
			return computeruse.ErrorResult("coordinate is required for left_click_drag action"), nil
		}
		if err := move("left_click_drag.start_coordinate", *raw.StartCoordinate); err != nil {
			return computeruse.ErrorResult(err.Error()), nil
		}
		if err := desktop.ExecVoid("mouse", "down", "left"); err != nil {
			return computeruse.ErrorResult(err.Error()), nil
		}
		if err := move("left_click_drag.coordinate", *raw.Coordinate); err != nil {
			return computeruse.ErrorResult(err.Error()), nil
		}
		if err := desktop.ExecVoid("mouse", "up", "left"); err != nil {
			return computeruse.ErrorResult(err.Error()), nil
		}
		return screenshot(nil)

	case anthropic.ActionLeftMouseDown:
		if raw.Coordinate != nil {
			if err := move("left_mouse_down.coordinate", *raw.Coordinate); err != nil {
				return computeruse.ErrorResult(err.Error()), nil
			}
		}
		if err := desktop.ExecVoid("mouse", "down", "left"); err != nil {
			return computeruse.ErrorResult(err.Error()), nil
		}
		return screenshot(nil)

	case anthropic.ActionLeftMouseUp:
		if raw.Coordinate != nil {
			if err := move("left_mouse_up.coordinate", *raw.Coordinate); err != nil {
				return computeruse.ErrorResult(err.Error()), nil
			}
		}
		if err := desktop.ExecVoid("mouse", "up", "left"); err != nil {
			return computeruse.ErrorResult(err.Error()), nil
		}
		return screenshot(nil)

	case anthropic.ActionRightClick:
		if raw.Coordinate != nil {
			if err := move("right_click.coordinate", *raw.Coordinate); err != nil {
				return computeruse.ErrorResult(err.Error()), nil
			}
		}
		if err := desktop.ExecVoid("mouse", "click", "right"); err != nil {
			return computeruse.ErrorResult(err.Error()), nil
		}
		if metrics != nil {
			metrics.Clicks++
		}
		return screenshot(nil)

	case anthropic.ActionMiddleClick:
		if raw.Coordinate != nil {
			if err := move("middle_click.coordinate", *raw.Coordinate); err != nil {
				return computeruse.ErrorResult(err.Error()), nil
			}
		}
		if err := desktop.ExecVoid("mouse", "click", "middle"); err != nil {
			return computeruse.ErrorResult(err.Error()), nil
		}
		if metrics != nil {
			metrics.Clicks++
		}
		return screenshot(nil)

	case anthropic.ActionDoubleClick:
		if raw.Coordinate != nil {
			if err := move("double_click.coordinate", *raw.Coordinate); err != nil {
				return computeruse.ErrorResult(err.Error()), nil
			}
		}
		for i := 0; i < 2; i++ {
			if err := desktop.ExecVoid("mouse", "click", "left"); err != nil {
				return computeruse.ErrorResult(err.Error()), nil
			}
			if metrics != nil {
				metrics.Clicks++
			}
		}
		return screenshot(nil)

	case anthropic.ActionTripleClick:
		if raw.Coordinate != nil {
			if err := move("triple_click.coordinate", *raw.Coordinate); err != nil {
				return computeruse.ErrorResult(err.Error()), nil
			}
		}
		for i := 0; i < 3; i++ {
			if err := desktop.ExecVoid("mouse", "click", "left"); err != nil {
				return computeruse.ErrorResult(err.Error()), nil
			}
			if metrics != nil {
				metrics.Clicks++
			}
		}
		return screenshot(nil)

	case anthropic.ActionScroll:
		if raw.Coordinate != nil {
			if err := move("scroll.coordinate", *raw.Coordinate); err != nil {
				return computeruse.ErrorResult(err.Error()), nil
			}
		}
		amount := int(input.ScrollAmount)
		if amount < 1 {
			amount = 3
		}
		dir := input.ScrollDirection
		if dir == "" {
			dir = "down"
		}
		var dx, dy int
		switch dir {
		case "up":
			dy = -amount
		case "down":
			dy = amount
		case "left":
			dx = -amount
		case "right":
			dx = amount
		}
		if err := desktop.ExecVoid("mouse", "scroll", strconv.Itoa(dx), strconv.Itoa(dy)); err != nil {
			return computeruse.ErrorResult(err.Error()), nil
		}
		return screenshot(nil)

	case anthropic.ActionWait:
		durSec := input.Duration
		if durSec < 1 {
			durSec = 1
		}
		if metrics != nil {
			metrics.Waits++
		}
		time.Sleep(time.Duration(max(10, int(durSec)*1000)) * time.Millisecond)
		return screenshot(nil)

	case anthropic.ActionScreenshot:
		return screenshot(nil)

	case anthropic.ActionZoom:
		if raw.Region == nil {
			return computeruse.ErrorResult("region is required for zoom action"), nil
		}
		region := &[4]int{
			int(raw.Region[0]),
			int(raw.Region[1]),
			int(raw.Region[2]),
			int(raw.Region[3]),
		}
		return screenshot(region)

	default:
		return computeruse.ErrorResult(fmt.Sprintf("unsupported action: %s", input.Action)), nil
	}
}
