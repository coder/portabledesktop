// Portable Desktop AI Agent Example (Go / Fantasy)
//
// Drives a virtual desktop via the `portabledesktop` CLI binary and lets
// Claude interact with it through Anthropic's computer-use tool protocol,
// using the Fantasy AI SDK for Go.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"

	"github.com/coder/portabledesktop/examples/agent-go/clickprobe"
	"github.com/coder/portabledesktop/examples/agent-go/desktop"
	"github.com/coder/portabledesktop/examples/agent-go/display"
	"github.com/coder/portabledesktop/examples/agent-go/reporting"
)

const (
	defaultPrompt         = "Open a browser, go to Hacker News, and tell me what the top comments on the top 3 stories are."
	defaultWidth          = 1280
	defaultHeight         = 800
	defaultViewerPort     = 6080
	defaultModel          = "claude-opus-4-6"
	defaultMaxSteps       = 100
	defaultScreenshotToMS = 20000
)

var (
	flagPrompt     = flag.String("prompt", defaultPrompt, "Prompt to send to the agent")
	flagModel      = flag.String("model", defaultModel, "Anthropic model ID")
	flagMaxSteps   = flag.Int("max-steps", defaultMaxSteps, "Maximum agent steps")
	flagClickprobe = flag.Bool("clickprobe", false, "Enable clickprobe mode (builds and runs the click-target test app)")

	activeGeometry = display.MustGeometry(defaultWidth, defaultHeight)
	screenshotsDir = ""
)

func SystemPrompt() string {
	return "Use the computer tool to complete the task."
}

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

func declaredCoordinateToNative(label string, coordinate [2]int64) image.Point {
	declaredPoint := image.Pt(int(coordinate[0]), int(coordinate[1]))
	nativePoint := activeGeometry.DeclaredPointToNative(declaredPoint)
	return nativePoint
}

func moveMouseToDeclaredCoordinate(label string, coordinate [2]int64, metrics *reporting.AgentMetrics) error {
	nativePoint := declaredCoordinateToNative(label, coordinate)
	if err := desktop.ExecVoid("mouse", "move", strconv.Itoa(nativePoint.X), strconv.Itoa(nativePoint.Y)); err != nil {
		return err
	}
	if metrics != nil {
		metrics.MouseMoves++
	}
	return nil
}

func executeComputerAction(
	input anthropic.ComputerUseInput,
	rawInput string,
	metrics *reporting.AgentMetrics,
) ([]fantasy.ToolResultOutputContent, error) {
	raw, err := parseRawComputerUseInput(rawInput)
	if err != nil {
		return errorResult(fmt.Sprintf("parse raw input: %v", err)), nil
	}

	switch input.Action {
	case anthropic.ActionKey:
		if input.Text == "" {
			return textResult("text is required for key action"), nil
		}
		if err := desktop.ExecVoid("keyboard", "key", input.Text); err != nil {
			return errorResult(err.Error()), nil
		}
		return captureScreenshotResult(metrics, nil)

	case anthropic.ActionHoldKey:
		if input.Text == "" {
			return errorResult("text is required for hold_key action"), nil
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
				return errorResult(err.Error()), nil
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
		return captureScreenshotResult(metrics, nil)

	case anthropic.ActionType:
		if input.Text == "" {
			return errorResult("text is required for type action"), nil
		}
		if err := desktop.ExecVoid("keyboard", "type", input.Text); err != nil {
			return errorResult(err.Error()), nil
		}
		return captureScreenshotResult(metrics, nil)

	case anthropic.ActionMouseMove:
		if raw.Coordinate == nil {
			return errorResult("coordinate is required for mouse_move action"), nil
		}
		if err := moveMouseToDeclaredCoordinate("mouse_move.coordinate", *raw.Coordinate, metrics); err != nil {
			return errorResult(err.Error()), nil
		}
		return captureScreenshotResult(metrics, nil)

	case anthropic.ActionLeftClick:
		if raw.Coordinate != nil {
			if err := moveMouseToDeclaredCoordinate("left_click.coordinate", *raw.Coordinate, metrics); err != nil {
				return errorResult(err.Error()), nil
			}
		}
		if err := desktop.ExecVoid("mouse", "click", "left"); err != nil {
			return errorResult(err.Error()), nil
		}
		if metrics != nil {
			metrics.Clicks++
		}
		return captureScreenshotResult(metrics, nil)

	case anthropic.ActionLeftClickDrag:
		if raw.StartCoordinate == nil {
			return errorResult("start_coordinate is required for left_click_drag action"), nil
		}
		if raw.Coordinate == nil {
			return errorResult("coordinate is required for left_click_drag action"), nil
		}
		if err := moveMouseToDeclaredCoordinate("left_click_drag.start_coordinate", *raw.StartCoordinate, metrics); err != nil {
			return errorResult(err.Error()), nil
		}
		if err := desktop.ExecVoid("mouse", "down", "left"); err != nil {
			return errorResult(err.Error()), nil
		}
		if err := moveMouseToDeclaredCoordinate("left_click_drag.coordinate", *raw.Coordinate, metrics); err != nil {
			return errorResult(err.Error()), nil
		}
		if err := desktop.ExecVoid("mouse", "up", "left"); err != nil {
			return errorResult(err.Error()), nil
		}
		return captureScreenshotResult(metrics, nil)

	case anthropic.ActionLeftMouseDown:
		if raw.Coordinate != nil {
			if err := moveMouseToDeclaredCoordinate("left_mouse_down.coordinate", *raw.Coordinate, metrics); err != nil {
				return errorResult(err.Error()), nil
			}
		}
		if err := desktop.ExecVoid("mouse", "down", "left"); err != nil {
			return errorResult(err.Error()), nil
		}
		return captureScreenshotResult(metrics, nil)

	case anthropic.ActionLeftMouseUp:
		if raw.Coordinate != nil {
			if err := moveMouseToDeclaredCoordinate("left_mouse_up.coordinate", *raw.Coordinate, metrics); err != nil {
				return errorResult(err.Error()), nil
			}
		}
		if err := desktop.ExecVoid("mouse", "up", "left"); err != nil {
			return errorResult(err.Error()), nil
		}
		return captureScreenshotResult(metrics, nil)

	case anthropic.ActionRightClick:
		if raw.Coordinate != nil {
			if err := moveMouseToDeclaredCoordinate("right_click.coordinate", *raw.Coordinate, metrics); err != nil {
				return errorResult(err.Error()), nil
			}
		}
		if err := desktop.ExecVoid("mouse", "click", "right"); err != nil {
			return errorResult(err.Error()), nil
		}
		if metrics != nil {
			metrics.Clicks++
		}
		return captureScreenshotResult(metrics, nil)

	case anthropic.ActionMiddleClick:
		if raw.Coordinate != nil {
			if err := moveMouseToDeclaredCoordinate("middle_click.coordinate", *raw.Coordinate, metrics); err != nil {
				return errorResult(err.Error()), nil
			}
		}
		if err := desktop.ExecVoid("mouse", "click", "middle"); err != nil {
			return errorResult(err.Error()), nil
		}
		if metrics != nil {
			metrics.Clicks++
		}
		return captureScreenshotResult(metrics, nil)

	case anthropic.ActionDoubleClick:
		if raw.Coordinate != nil {
			if err := moveMouseToDeclaredCoordinate("double_click.coordinate", *raw.Coordinate, metrics); err != nil {
				return errorResult(err.Error()), nil
			}
		}
		if err := desktop.ExecVoid("mouse", "click", "left"); err != nil {
			return errorResult(err.Error()), nil
		}
		if metrics != nil {
			metrics.Clicks++
		}
		if err := desktop.ExecVoid("mouse", "click", "left"); err != nil {
			return errorResult(err.Error()), nil
		}
		if metrics != nil {
			metrics.Clicks++
		}
		return captureScreenshotResult(metrics, nil)

	case anthropic.ActionTripleClick:
		if raw.Coordinate != nil {
			if err := moveMouseToDeclaredCoordinate("triple_click.coordinate", *raw.Coordinate, metrics); err != nil {
				return errorResult(err.Error()), nil
			}
		}
		if err := desktop.ExecVoid("mouse", "click", "left"); err != nil {
			return errorResult(err.Error()), nil
		}
		if metrics != nil {
			metrics.Clicks++
		}
		if err := desktop.ExecVoid("mouse", "click", "left"); err != nil {
			return errorResult(err.Error()), nil
		}
		if metrics != nil {
			metrics.Clicks++
		}
		if err := desktop.ExecVoid("mouse", "click", "left"); err != nil {
			return errorResult(err.Error()), nil
		}
		if metrics != nil {
			metrics.Clicks++
		}
		return captureScreenshotResult(metrics, nil)

	case anthropic.ActionScroll:
		if raw.Coordinate != nil {
			if err := moveMouseToDeclaredCoordinate("scroll.coordinate", *raw.Coordinate, metrics); err != nil {
				return errorResult(err.Error()), nil
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
			return errorResult(err.Error()), nil
		}
		return captureScreenshotResult(metrics, nil)

	case anthropic.ActionWait:
		durSec := input.Duration
		if durSec < 1 {
			durSec = 1
		}
		if metrics != nil {
			metrics.Waits++
		}
		time.Sleep(time.Duration(max(10, int(durSec)*1000)) * time.Millisecond)
		return captureScreenshotResult(metrics, nil)

	case anthropic.ActionScreenshot:
		return captureScreenshotResult(metrics, nil)

	case anthropic.ActionZoom:
		if raw.Region == nil {
			return errorResult("region is required for zoom action"), nil
		}
		region := &[4]int{
			int(raw.Region[0]),
			int(raw.Region[1]),
			int(raw.Region[2]),
			int(raw.Region[3]),
		}
		return captureScreenshotResult(metrics, region)

	default:
		return errorResult(fmt.Sprintf("unsupported action: %s", input.Action)), nil
	}
}

func textResult(text string) []fantasy.ToolResultOutputContent {
	return []fantasy.ToolResultOutputContent{
		fantasy.ToolResultOutputContentText{Text: text},
	}
}

func errorResult(text string) []fantasy.ToolResultOutputContent {
	return []fantasy.ToolResultOutputContent{
		fantasy.ToolResultOutputContentError{Error: fmt.Errorf("%s", text)},
	}
}

func captureScreenshotResult(metrics *reporting.AgentMetrics, region *[4]int) ([]fantasy.ToolResultOutputContent, error) {
	targetWidth := activeGeometry.DeclaredWidth
	targetHeight := activeGeometry.DeclaredHeight

	// Always capture a full screenshot at the declared display size.
	args := []string{
		"screenshot",
		"--json",
		"--target-width", strconv.Itoa(targetWidth),
		"--target-height", strconv.Itoa(targetHeight),
		"--timeout-ms", strconv.Itoa(defaultScreenshotToMS),
	}

	out, err := desktop.Exec(args...)
	if err != nil {
		return errorResult(fmt.Sprintf("screenshot: %v", err)), nil
	}

	var result struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &result); err != nil {
		return errorResult(fmt.Sprintf("parse screenshot: %v", err)), nil
	}
	pngData, err := base64.StdEncoding.DecodeString(result.Data)
	if err != nil {
		return errorResult(fmt.Sprintf("invalid base64 screenshot: %v", err)), nil
	}
	actualWidth, actualHeight, err := display.ParsePNGDimensions(pngData)
	if err != nil {
		return errorResult(fmt.Sprintf("parse screenshot dimensions: %v", err)), nil
	}
	if err := display.ValidateScreenshotDimensions(
		actualWidth,
		actualHeight,
		activeGeometry.DeclaredWidth,
		activeGeometry.DeclaredHeight,
	); err != nil {
		fmt.Fprintf(os.Stderr, "    screenshot dimension mismatch: %v\n", err)
		return errorResult(err.Error()), nil
	}

	// For zoom: crop the declared-coordinate region from the full
	// screenshot, scale it up to ~1M pixels so the model gets a
	// detailed view, and return the result.
	if region != nil {
		pngData, err = display.CropDeclaredRegion(pngData, *region)
		if err != nil {
			return errorResult(fmt.Sprintf("crop zoom region: %v", err)), nil
		}
		pngData, err = display.ScaleToTargetPixels(pngData, 250_000)
		if err != nil {
			return errorResult(fmt.Sprintf("scale zoom screenshot: %v", err)), nil
		}
		result.Data = base64.StdEncoding.EncodeToString(pngData)
		cropW, cropH, _ := display.ParsePNGDimensions(pngData)
		fmt.Fprintf(
			os.Stderr,
			"    screenshot zoom crop: %dx%d\n",
			cropW, cropH,
		)
	} else {
		fmt.Fprintf(
			os.Stderr,
			"    screenshot returned: actual=%dx%d declared=%dx%d\n",
			actualWidth,
			actualHeight,
			activeGeometry.DeclaredWidth,
			activeGeometry.DeclaredHeight,
		)
	}

	if metrics != nil {
		metrics.Screenshots++
	}

	// Save the screenshot to disk for debugging.
	if screenshotsDir != "" {
		seq := 0
		if metrics != nil {
			seq = metrics.Screenshots
		}
		var filename string
		if region == nil {
			filename = fmt.Sprintf("%04d_full.png", seq)
		} else {
			filename = fmt.Sprintf(
				"%04d_zoom_%d_%d_%d_%d.png",
				seq, region[0], region[1], region[2], region[3],
			)
		}
		if err := os.WriteFile(
			filepath.Join(screenshotsDir, filename),
			pngData,
			0o644,
		); err != nil {
			fmt.Fprintf(os.Stderr, "    warning: save screenshot %s: %v\n", filename, err)
		}
	}

	return []fantasy.ToolResultOutputContent{
		fantasy.ToolResultOutputContentMedia{Data: result.Data, MediaType: "image/png"},
	}, nil
}

func runAgentLoop(
	ctx context.Context,
	model fantasy.LanguageModel,
	computerTool fantasy.ProviderDefinedTool,
	prompt string,
	maxSteps int,
	messagesPath string,
	metrics *reporting.AgentMetrics,
) error {
	systemMsg := fantasy.NewSystemMessage(SystemPrompt())

	messages := fantasy.Prompt{
		systemMsg,
		fantasy.NewUserMessage(prompt),
	}
	tools := []fantasy.Tool{computerTool}

	reporting.SaveMessages(messagesPath, messages)

	for step := 0; step < maxSteps; step++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		resp, err := model.Generate(ctx, fantasy.Call{Prompt: messages, Tools: tools})
		if err != nil {
			return fmt.Errorf("generate (step %d): %w", step, err)
		}

		var toolCalls []fantasy.ToolCallContent
		for _, c := range resp.Content {
			switch c.GetType() {
			case fantasy.ContentTypeText:
				if tc, ok := fantasy.AsContentType[fantasy.TextContent](c); ok && tc.Text != "" {
					fmt.Print(tc.Text)
				}
			case fantasy.ContentTypeToolCall:
				if tc, ok := fantasy.AsContentType[fantasy.ToolCallContent](c); ok {
					toolCalls = append(toolCalls, tc)
				}
			}
		}

		if len(toolCalls) == 0 {
			fmt.Println()
			return nil
		}

		var assistantParts []fantasy.MessagePart
		for _, c := range resp.Content {
			switch c.GetType() {
			case fantasy.ContentTypeText:
				if tc, ok := fantasy.AsContentType[fantasy.TextContent](c); ok {
					assistantParts = append(assistantParts, fantasy.TextPart{Text: tc.Text})
				}
			case fantasy.ContentTypeToolCall:
				if tc, ok := fantasy.AsContentType[fantasy.ToolCallContent](c); ok {
					assistantParts = append(assistantParts, fantasy.ToolCallPart{
						ToolCallID: tc.ToolCallID,
						ToolName:   tc.ToolName,
						Input:      tc.Input,
					})
				}
			}
		}
		messages = append(messages, fantasy.Message{
			Role:    fantasy.MessageRoleAssistant,
			Content: assistantParts,
		})

		var toolResultParts []fantasy.MessagePart
		for _, tc := range toolCalls {
			if metrics != nil {
				metrics.ToolCalls++
			}
			fmt.Fprintf(os.Stderr, "  [step %d] tool: %s (id=%s)\n", step, tc.ToolName, tc.ToolCallID)

			action, err := anthropic.ParseComputerUseInput(tc.Input)
			if err != nil {
				toolResultParts = append(toolResultParts, fantasy.ToolResultPart{
					ToolCallID: tc.ToolCallID,
					Output:     fantasy.ToolResultOutputContentText{Text: fmt.Sprintf("invalid input: %v", err)},
				})
				continue
			}

			fmt.Fprintf(os.Stderr, "  [step %d] action: %s\n", step, reporting.FormatAction(action))
			start := time.Now()
			results, err := executeComputerAction(action, tc.Input, metrics)
			elapsed := time.Since(start)
			fmt.Fprintf(os.Stderr, "  [step %d] action executed in %s\n", step, elapsed)
			if err != nil {
				toolResultParts = append(toolResultParts, fantasy.ToolResultPart{
					ToolCallID: tc.ToolCallID,
					Output:     fantasy.ToolResultOutputContentText{Text: fmt.Sprintf("error: %v", err)},
				})
				continue
			}
			if len(results) > 0 {
				toolResultParts = append(toolResultParts, fantasy.ToolResultPart{
					ToolCallID: tc.ToolCallID,
					Output:     results[0],
				})
			}
		}

		messages = append(messages, fantasy.Message{
			Role:    fantasy.MessageRoleTool,
			Content: toolResultParts,
		})
		reporting.SaveMessages(messagesPath, messages)

		if resp.FinishReason != fantasy.FinishReasonToolCalls {
			return nil
		}
	}

	fmt.Fprintf(os.Stderr, "reached max steps (%d)\n", maxSteps)
	return nil
}

func entrypoint() (retErr error) {
	flag.Parse()
	desktop.LoadEnvLocal()

	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY is missing. Set it in environment or .env.local at repo root.")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		sig := <-sigCh
		fmt.Fprintf(os.Stderr, "\nreceived %s, shutting down...\n", sig)
		cancel()
	}()

	fmt.Println("starting portable desktop...")
	session, err := desktop.StartDesktop(fmt.Sprintf("%dx%d", defaultWidth, defaultHeight), "#1f252f")
	if err != nil {
		return fmt.Errorf("start desktop: %w", err)
	}
	desktop.ActiveStateFile = session.Info.StateFile
	defer session.Stop()

	activeGeometry, err = display.ParseSessionGeometry(session.Info.Geometry)
	if err != nil {
		return fmt.Errorf("resolve active geometry: %w", err)
	}

	fmt.Printf("display :%d  vnc :%d  geometry %s\n", session.Info.Display, session.Info.VNCPort, session.Info.Geometry)
	fmt.Printf(
		"native geometry: %dx%d  declared geometry: %dx%d\n",
		activeGeometry.NativeWidth,
		activeGeometry.NativeHeight,
		activeGeometry.DeclaredWidth,
		activeGeometry.DeclaredHeight,
	)

	tmpDir := filepath.Join("tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("create tmp dir: %w", err)
	}
	recordingPath, err := filepath.Abs(filepath.Join(tmpDir, fmt.Sprintf("agent-%d.mp4", time.Now().UnixMilli())))
	if err != nil {
		return fmt.Errorf("resolve recording path: %w", err)
	}
	messagesPath := strings.TrimSuffix(recordingPath, filepath.Ext(recordingPath)) + ".json"

	runName := strings.TrimSuffix(filepath.Base(recordingPath), filepath.Ext(recordingPath))
	screenshotsDir = filepath.Join(tmpDir, runName+"-screenshots")
	if err := os.MkdirAll(screenshotsDir, 0o755); err != nil {
		return fmt.Errorf("create screenshots dir: %w", err)
	}
	fmt.Printf("screenshots: %s\n", screenshotsDir)

	var cpRuntime *clickprobe.Runtime
	summary := reporting.Summary{
		Model:         *flagModel,
		MaxSteps:      *flagMaxSteps,
		RecordingPath: recordingPath,
		MessagesPath:  messagesPath,
		Geometry:      activeGeometry,
	}
	defer func() {
		summary.RunErr = retErr
		summary.Clickprobe = clickprobe.BuildSummary(cpRuntime, summary.Geometry, defaultWidth, defaultHeight)
		reporting.PrintSummary(summary)
	}()

	recording := desktop.StartRecording(recordingPath)
	defer recording.Stop()
	fmt.Printf("recording: %s\n", recordingPath)

	viewerCmd := desktop.StartViewer(defaultViewerPort)
	defer desktop.StopProcess(viewerCmd)
	viewerURL := fmt.Sprintf("http://127.0.0.1:%d", defaultViewerPort)
	fmt.Printf("viewer: %s\n", viewerURL)
	desktop.OpenHostBrowser(viewerURL)

	if *flagClickprobe {
		cpRuntime, err = clickprobe.StartMode(ctx, session.Info.SessionDir, &activeGeometry, desktop.Exec)
		if err != nil {
			return err
		}
		summary.Geometry = activeGeometry
	}

	provider, err := anthropic.New(anthropic.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")))
	if err != nil {
		return fmt.Errorf("create provider: %w", err)
	}
	model, err := provider.LanguageModel(ctx, *flagModel)
	if err != nil {
		return fmt.Errorf("get language model: %w", err)
	}

	enableZoom := true
	computerTool := anthropic.NewComputerUseTool(anthropic.ComputerUseToolOptions{
		DisplayWidthPx:  int64(activeGeometry.DeclaredWidth),
		DisplayHeightPx: int64(activeGeometry.DeclaredHeight),
		EnableZoom:      &enableZoom,
		ToolVersion:     anthropic.ComputerUse20251124,
	})
	fmt.Printf(
		"computer tool declared display: %dx%d\n",
		activeGeometry.DeclaredWidth,
		activeGeometry.DeclaredHeight,
	)

	promptToRun := *flagPrompt
	if *flagClickprobe {
		promptToRun = clickprobe.Prompt
	}
	fmt.Printf("provider: anthropic  model: %s  max steps: %d\n", *flagModel, *flagMaxSteps)
	fmt.Printf("prompt: %q\n", promptToRun)
	fmt.Printf("messages: %s\n\n", messagesPath)
	fmt.Println("agent output (streaming):")

	if err := runAgentLoop(
		ctx,
		model,
		computerTool,
		promptToRun,
		*flagMaxSteps,
		messagesPath,
		&summary.AgentMetrics,
	); err != nil {
		return fmt.Errorf("agent loop: %w", err)
	}

	recording.Stop()
	fmt.Printf("\nsaved recording: %s\n", recordingPath)
	return nil
}

func main() {
	if err := entrypoint(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
