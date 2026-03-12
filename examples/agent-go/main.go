// Portable Desktop AI Agent Example (Go / Fantasy)
//
// Drives a virtual desktop via the `portabledesktop` CLI binary and
// lets Claude interact with it through Anthropic's computer-use tool
// protocol, using the Fantasy AI SDK for Go.
//
// Usage:
//
//	go run . --prompt "Open coder.com and confirm the homepage title."
//	PORTABLEDESKTOP_BIN=/path/to/portabledesktop go run . --prompt "Do something."
package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	defaultPrompt         = "Navigate to news.ycombinator.com and tell me what the top comments on the top 3 stories are. Never wait for a page to load more than 1 second."
	defaultWidth          = 1280
	defaultHeight         = 800
	defaultViewerPort     = 6080
	defaultModel          = "claude-opus-4-6"
	defaultMaxSteps       = 100
	defaultScreenshotToMS = 20000

	// Anthropic recommended screenshot limits.
	maxScreenshotLongEdge = 1568
	maxScreenshotPixels   = 1_150_000
)

// ---------------------------------------------------------------------------
// CLI flags
// ---------------------------------------------------------------------------

var (
	flagPrompt   = flag.String("prompt", defaultPrompt, "Prompt to send to the agent")
	flagModel    = flag.String("model", defaultModel, "Anthropic model ID")
	flagMaxSteps = flag.Int("max-steps", defaultMaxSteps, "Maximum agent steps")
)

func portabledesktopBin() string {
	if v := os.Getenv("PORTABLEDESKTOP_BIN"); v != "" {
		return v
	}
	return "portabledesktop"
}

// ---------------------------------------------------------------------------
// .env.local loader
// ---------------------------------------------------------------------------

func loadEnvLocal() {
	candidates := []string{
		filepath.Join("..", "..", ".env.local"),
		filepath.Join("..", "..", "..", ".env.local"),
	}
	for _, p := range candidates {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			line = strings.TrimPrefix(line, "export ")
			idx := strings.Index(line, "=")
			if idx == -1 {
				continue
			}
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			// Strip surrounding quotes.
			if len(val) >= 2 &&
				((val[0] == '"' && val[len(val)-1] == '"') ||
					(val[0] == '\'' && val[len(val)-1] == '\'')) {
				val = val[1 : len(val)-1]
			}
			if os.Getenv(key) == "" {
				os.Setenv(key, val)
			}
		}
		break
	}
}

// ---------------------------------------------------------------------------
// Desktop session — wraps the portabledesktop CLI lifecycle
// ---------------------------------------------------------------------------

type desktopInfo struct {
	RuntimeDir              string `json:"runtimeDir"`
	Display                 int    `json:"display"`
	VNCPort                 int    `json:"vncPort"`
	Geometry                string `json:"geometry"`
	Depth                   int    `json:"depth"`
	DPI                     int    `json:"dpi"`
	DesktopSizeMode         string `json:"desktopSizeMode"`
	SessionDir              string `json:"sessionDir"`
	CleanupSessionDirOnStop bool   `json:"cleanupSessionDirOnStop"`
	Detached                bool   `json:"detached"`
	StateFile               string `json:"stateFile"`
	StartedAt               string `json:"startedAt"`
}

type desktopSession struct {
	info *desktopInfo
	cmd  *exec.Cmd
}

func startDesktop(geometry, background string) (*desktopSession, error) {
	bin := portabledesktopBin()
	args := []string{"up", "--json", "--foreground"}
	if geometry != "" {
		args = append(args, "--geometry", geometry)
	}
	if background != "" {
		args = append(args, "--background", background)
	}

	cmd := exec.Command(bin, args...)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start portabledesktop: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("no output from portabledesktop up")
	}

	var info desktopInfo
	if err := json.Unmarshal(scanner.Bytes(), &info); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("parse desktop info: %w", err)
	}

	return &desktopSession{info: &info, cmd: cmd}, nil
}

func (s *desktopSession) stop() {
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Signal(syscall.SIGTERM)
		_ = s.cmd.Wait()
	}
}

// ---------------------------------------------------------------------------
// CLI exec helpers
// ---------------------------------------------------------------------------

func pdExec(args ...string) (string, error) {
	cmd := exec.Command(portabledesktopBin(), args...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("%s: %s", err, string(ee.Stderr))
		}
		return "", err
	}
	return string(out), nil
}

func pdExecVoid(args ...string) error {
	_, err := pdExec(args...)
	return err
}

// ---------------------------------------------------------------------------
// Recording
// ---------------------------------------------------------------------------

type recordingHandle struct {
	cmd *exec.Cmd
}

func startRecording(file string) *recordingHandle {
	cmd := exec.Command(portabledesktopBin(),
		"record",
		"--idle-speedup", "20",
		"--idle-min-duration", "0.35",
		"--idle-noise-tolerance", "-38dB",
		file,
	)
	cmd.Stdout = nil
	cmd.Stderr = nil
	_ = cmd.Start()
	return &recordingHandle{cmd: cmd}
}

func (r *recordingHandle) stop() {
	if r.cmd != nil && r.cmd.Process != nil {
		_ = r.cmd.Process.Signal(syscall.SIGINT)
		_ = r.cmd.Wait()
	}
}

// ---------------------------------------------------------------------------
// Viewer
// ---------------------------------------------------------------------------

func startViewer(port int) *exec.Cmd {
	cmd := exec.Command(portabledesktopBin(),
		"viewer",
		"--port", strconv.Itoa(port),
		"--host", "127.0.0.1",
		"--no-open",
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	_ = cmd.Start()
	return cmd
}

// ---------------------------------------------------------------------------
// Host browser opener
// ---------------------------------------------------------------------------

func openHostBrowser(url string) {
	var commands [][]string
	if runtime.GOOS == "darwin" {
		commands = [][]string{{"open", url}}
	} else {
		commands = [][]string{
			{"xdg-open", url},
			{"sensible-browser", url},
		}
	}
	for _, c := range commands {
		cmd := exec.Command(c[0], c[1:]...)
		if cmd.Start() == nil {
			_ = cmd.Process.Release()
			return
		}
	}
	fmt.Fprintf(os.Stdout, "  Open manually: %s\n", url)
}

// ---------------------------------------------------------------------------
// Desktop browser launcher
// ---------------------------------------------------------------------------

func resolveDesktopBrowser() string {
	candidates := []string{
		"google-chrome-stable",
		"google-chrome",
		"chromium-browser",
		"chromium",
		"firefox",
	}
	for _, name := range candidates {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

func launchDesktopBrowser(url string) error {
	browser := resolveDesktopBrowser()
	if browser == "" {
		return fmt.Errorf("no browser found inside the desktop")
	}

	args := []string{browser, "--no-first-run", "--disable-session-crashed-bubble"}
	base := filepath.Base(browser)
	if strings.Contains(base, "chrom") {
		args = append(args,
			"--disable-infobars",
			"--no-default-browser-check",
			fmt.Sprintf("--window-size=%d,%d", defaultWidth, defaultHeight),
			url,
		)
	} else {
		args = append(args, url)
	}

	allArgs := append([]string{"open", "--"}, args...)
	return pdExecVoid(allArgs...)
}

// ---------------------------------------------------------------------------
// Screenshot sizing
// ---------------------------------------------------------------------------

func computeScaledSize(w, h int) (int, int) {
	longEdge := float64(max(w, h))
	totalPx := float64(w * h)

	longEdgeScale := float64(maxScreenshotLongEdge) / longEdge
	totalScale := math.Sqrt(float64(maxScreenshotPixels) / totalPx)
	scale := math.Min(1, math.Min(longEdgeScale, totalScale))

	if scale >= 1 {
		return w, h
	}
	return max(1, int(math.Floor(float64(w)*scale))),
		max(1, int(math.Floor(float64(h)*scale)))
}

// ---------------------------------------------------------------------------
// Computer action types (matching Anthropic computer_20251124)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Computer tool execution
// ---------------------------------------------------------------------------

func clampCoord(x, y int) (int, int) {
	return max(0, min(defaultWidth-1, x)), max(0, min(defaultHeight-1, y))
}

// executeComputerAction runs a single computer action and returns a tool
// result that can be fed back into the conversation.
func executeComputerAction(input anthropic.ComputerUseInput) ([]fantasy.ToolResultOutputContent, error) {
	switch input.Action {
	case anthropic.ActionKey:
		if input.Text == "" {
			return textResult("text is required for key action"), nil
		}
		if err := pdExecVoid("keyboard", "key", input.Text); err != nil {
			return errorResult(err.Error()), nil
		}
		return captureScreenshotResult(nil)

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
			if err := pdExecVoid("keyboard", "down", k); err != nil {
				for i := len(pressed) - 1; i >= 0; i-- {
					_ = pdExecVoid("keyboard", "up", pressed[i])
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
			_ = pdExecVoid("keyboard", "up", pressed[i])
		}
		return captureScreenshotResult(nil)

	case anthropic.ActionType:
		if input.Text == "" {
			return errorResult("text is required for type action"), nil
		}
		if err := pdExecVoid("keyboard", "type", input.Text); err != nil {
			return errorResult(err.Error()), nil
		}
		return captureScreenshotResult(nil)

	case anthropic.ActionMouseMove:
		x, y := clampCoord(int(input.Coordinate[0]), int(input.Coordinate[1]))
		if err := pdExecVoid("mouse", "move", strconv.Itoa(x), strconv.Itoa(y)); err != nil {
			return errorResult(err.Error()), nil
		}
		return captureScreenshotResult(nil)

	case anthropic.ActionLeftClick:
		x, y := clampCoord(int(input.Coordinate[0]), int(input.Coordinate[1]))
		if err := pdExecVoid("mouse", "move", strconv.Itoa(x), strconv.Itoa(y)); err != nil {
			return errorResult(err.Error()), nil
		}
		if err := pdExecVoid("mouse", "click", "left"); err != nil {
			return errorResult(err.Error()), nil
		}
		return captureScreenshotResult(nil)

	case anthropic.ActionLeftClickDrag:
		sx, sy := clampCoord(int(input.StartCoordinate[0]), int(input.StartCoordinate[1]))
		ex, ey := clampCoord(int(input.Coordinate[0]), int(input.Coordinate[1]))
		_ = pdExecVoid("mouse", "move", strconv.Itoa(sx), strconv.Itoa(sy))
		_ = pdExecVoid("mouse", "down", "left")
		_ = pdExecVoid("mouse", "move", strconv.Itoa(ex), strconv.Itoa(ey))
		_ = pdExecVoid("mouse", "up", "left")
		return captureScreenshotResult(nil)

	case anthropic.ActionLeftMouseDown:
		if err := pdExecVoid("mouse", "down", "left"); err != nil {
			return errorResult(err.Error()), nil
		}
		return captureScreenshotResult(nil)

	case anthropic.ActionLeftMouseUp:
		if err := pdExecVoid("mouse", "up", "left"); err != nil {
			return errorResult(err.Error()), nil
		}
		return captureScreenshotResult(nil)

	case anthropic.ActionRightClick:
		x, y := clampCoord(int(input.Coordinate[0]), int(input.Coordinate[1]))
		_ = pdExecVoid("mouse", "move", strconv.Itoa(x), strconv.Itoa(y))
		if err := pdExecVoid("mouse", "click", "right"); err != nil {
			return errorResult(err.Error()), nil
		}
		return captureScreenshotResult(nil)

	case anthropic.ActionMiddleClick:
		x, y := clampCoord(int(input.Coordinate[0]), int(input.Coordinate[1]))
		_ = pdExecVoid("mouse", "move", strconv.Itoa(x), strconv.Itoa(y))
		if err := pdExecVoid("mouse", "click", "middle"); err != nil {
			return errorResult(err.Error()), nil
		}
		return captureScreenshotResult(nil)

	case anthropic.ActionDoubleClick:
		x, y := clampCoord(int(input.Coordinate[0]), int(input.Coordinate[1]))
		_ = pdExecVoid("mouse", "move", strconv.Itoa(x), strconv.Itoa(y))
		_ = pdExecVoid("mouse", "click", "left")
		_ = pdExecVoid("mouse", "click", "left")
		return captureScreenshotResult(nil)

	case anthropic.ActionTripleClick:
		x, y := clampCoord(int(input.Coordinate[0]), int(input.Coordinate[1]))
		_ = pdExecVoid("mouse", "move", strconv.Itoa(x), strconv.Itoa(y))
		_ = pdExecVoid("mouse", "click", "left")
		_ = pdExecVoid("mouse", "click", "left")
		_ = pdExecVoid("mouse", "click", "left")
		return captureScreenshotResult(nil)

	case anthropic.ActionScroll:
		x, y := clampCoord(int(input.Coordinate[0]), int(input.Coordinate[1]))
		_ = pdExecVoid("mouse", "move", strconv.Itoa(x), strconv.Itoa(y))
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
		if err := pdExecVoid("mouse", "scroll", strconv.Itoa(dx), strconv.Itoa(dy)); err != nil {
			return errorResult(err.Error()), nil
		}
		return captureScreenshotResult(nil)

	case anthropic.ActionWait:
		durSec := input.Duration
		if durSec < 1 {
			durSec = 1
		}
		ms := max(10, int(durSec)*1000)
		time.Sleep(time.Duration(ms) * time.Millisecond)
		return captureScreenshotResult(nil)

	case anthropic.ActionScreenshot:
		return captureScreenshotResult(nil)

	case anthropic.ActionZoom:
		region := &[4]int{int(input.Region[0]), int(input.Region[1]), int(input.Region[2]), int(input.Region[3])}
		return captureScreenshotResult(region)

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

// captureScreenshotResult takes a screenshot and returns it as a tool
// result containing a base64-encoded PNG image.
func captureScreenshotResult(region *[4]int) ([]fantasy.ToolResultOutputContent, error) {
	tw, th := computeScaledSize(defaultWidth, defaultHeight)

	args := []string{
		"screenshot",
		"--json",
		"--target-width", strconv.Itoa(tw),
		"--target-height", strconv.Itoa(th),
	}

	if region != nil {
		left := max(0, min(region[0], region[2]))
		top := max(0, min(region[1], region[3]))
		right := min(defaultWidth, max(region[0], region[2]))
		bottom := min(defaultHeight, max(region[1], region[3]))
		w := right - left
		h := bottom - top
		if w > 0 && h > 0 {
			args = append(args,
				"--x", strconv.Itoa(left),
				"--y", strconv.Itoa(top),
				"--width", strconv.Itoa(w),
				"--height", strconv.Itoa(h),
				"--scale-to-geometry",
			)
		}
	}

	args = append(args, "--timeout-ms", strconv.Itoa(defaultScreenshotToMS))

	out, err := pdExec(args...)
	if err != nil {
		return errorResult(fmt.Sprintf("screenshot: %v", err)), nil
	}

	var result struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &result); err != nil {
		return errorResult(fmt.Sprintf("parse screenshot: %v", err)), nil
	}

	// Decode to verify, then return as base64 media content.
	if _, err := base64.StdEncoding.DecodeString(result.Data); err != nil {
		return errorResult(fmt.Sprintf("invalid base64 screenshot: %v", err)), nil
	}

	return []fantasy.ToolResultOutputContent{
		fantasy.ToolResultOutputContentMedia{
			Data:      result.Data,
			MediaType: "image/png",
		},
	}, nil
}

// formatAction returns a human-readable summary of the non-empty
// fields in a computerAction, suitable for log output.
func formatAction(a anthropic.ComputerUseInput) string {
	marshalled, err := json.Marshal(a)
	if err != nil {
		return fmt.Sprintf("error marshalling action: %v", err)
	}
	return string(marshalled)
}

// ---------------------------------------------------------------------------
// Agent loop — drives model.Generate with tool results fed back
// ---------------------------------------------------------------------------

func saveMessages(path string, messages fantasy.Prompt) {
	data, err := json.MarshalIndent(messages, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to marshal messages: %v\n", err)
		return
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to write messages: %v\n", err)
	}
}

func runAgentLoop(ctx context.Context, model fantasy.LanguageModel, computerTool fantasy.ProviderDefinedTool, prompt string, maxSteps int, messagesPath string) error {
	systemMsg := fantasy.NewSystemMessage(
		"Use the computer tool to complete the user prompt in the already-open browser window. " +
			"Prefer direct actions and keep steps concise. Do not ask any questions, just perform the task.",
	)

	messages := fantasy.Prompt{
		systemMsg,
		fantasy.NewUserMessage(prompt),
	}

	tools := []fantasy.Tool{computerTool}

	saveMessages(messagesPath, messages)

	for step := 0; step < maxSteps; step++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		resp, err := model.Generate(ctx, fantasy.Call{
			Prompt: messages,
			Tools:  tools,
		})
		if err != nil {
			return fmt.Errorf("generate (step %d): %w", step, err)
		}

		// Collect tool calls and any text from the response.
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

		// If no tool calls, the model is done.
		if len(toolCalls) == 0 {
			fmt.Println()
			return nil
		}

		// Build assistant message with the tool calls.
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

		// Execute each tool call and build tool result messages.
		var toolResultParts []fantasy.MessagePart
		for _, tc := range toolCalls {
			fmt.Fprintf(os.Stderr, "  [step %d] tool: %s (id=%s)\n", step, tc.ToolName, tc.ToolCallID)

			action, err := anthropic.ParseComputerUseInput(tc.Input)
			if err != nil {
				toolResultParts = append(toolResultParts, fantasy.ToolResultPart{
					ToolCallID: tc.ToolCallID,
					Output:     fantasy.ToolResultOutputContentText{Text: fmt.Sprintf("invalid input: %v", err)},
				})
				continue
			}

			fmt.Fprintf(os.Stderr, "  [step %d] action: %s\n", step, formatAction(action))
			start := time.Now()
			results, err := executeComputerAction(action)
			elapsed := time.Since(start)
			fmt.Fprintf(os.Stderr, "  [step %d] action executed in %s\n", step, elapsed)
			if err != nil {
				toolResultParts = append(toolResultParts, fantasy.ToolResultPart{
					ToolCallID: tc.ToolCallID,
					Output:     fantasy.ToolResultOutputContentText{Text: fmt.Sprintf("error: %v", err)},
				})
				continue
			}

			// Use the first result part as the output.
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

		saveMessages(messagesPath, messages)

		// If the model didn't finish because of tool calls, stop.
		if resp.FinishReason != fantasy.FinishReasonToolCalls {
			return nil
		}
	}

	fmt.Fprintf(os.Stderr, "reached max steps (%d)\n", maxSteps)
	return nil
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func entrypoint() error {
	flag.Parse()
	loadEnvLocal()

	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY is missing. Set it in environment or .env.local at repo root.")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle SIGINT/SIGTERM for graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		fmt.Fprintf(os.Stderr, "\nreceived %s, shutting down...\n", sig)
		cancel()
	}()

	fmt.Println("starting portable desktop...")
	session, err := startDesktop(
		fmt.Sprintf("%dx%d", defaultWidth, defaultHeight),
		"#1f252f",
	)
	if err != nil {
		return fmt.Errorf("start desktop: %w", err)
	}
	defer session.stop()

	fmt.Printf("display :%d  vnc :%d  geometry %s\n",
		session.info.Display, session.info.VNCPort, session.info.Geometry)

	// Start recording.
	tmpDir := filepath.Join("tmp")
	_ = os.MkdirAll(tmpDir, 0o755)
	recordingPath, _ := filepath.Abs(
		filepath.Join(tmpDir, fmt.Sprintf("agent-%d.mp4", time.Now().UnixMilli())),
	)
	recording := startRecording(recordingPath)
	fmt.Printf("recording: %s\n", recordingPath)

	// Start the viewer.
	viewerCmd := startViewer(defaultViewerPort)
	defer func() {
		if viewerCmd.Process != nil {
			_ = viewerCmd.Process.Kill()
		}
	}()
	viewerURL := fmt.Sprintf("http://127.0.0.1:%d", defaultViewerPort)
	fmt.Printf("viewer: %s\n", viewerURL)
	openHostBrowser(viewerURL)

	// Let the desktop settle, then launch a browser inside it.
	time.Sleep(1500 * time.Millisecond)
	if err := launchDesktopBrowser("about:blank"); err != nil {
		return fmt.Errorf("launch desktop browser: %w", err)
	}
	time.Sleep(2000 * time.Millisecond)

	// Set up the Anthropic provider and model.
	provider, err := anthropic.New(anthropic.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")))
	if err != nil {
		return fmt.Errorf("create provider: %w", err)
	}

	model, err := provider.LanguageModel(ctx, *flagModel)
	if err != nil {
		return fmt.Errorf("get language model: %w", err)
	}

	// Create the computer use tool (provider-defined) for the model.
	displayNum := int64(session.info.Display)
	enableZoom := true
	computerTool := anthropic.NewComputerUseTool(anthropic.ComputerUseToolOptions{
		DisplayWidthPx:  int64(defaultWidth),
		DisplayHeightPx: int64(defaultHeight),
		DisplayNumber:   &displayNum,
		EnableZoom:      &enableZoom,
		ToolVersion:     anthropic.ComputerUse20251124,
	})

	fmt.Printf("provider: anthropic  model: %s  max steps: %d\n", *flagModel, *flagMaxSteps)
	fmt.Printf("prompt: %q\n\n", *flagPrompt)
	fmt.Println("agent output (streaming):")

	// Derive messages log path from the recording path (same base, .json).
	messagesPath := strings.TrimSuffix(recordingPath, filepath.Ext(recordingPath)) + ".json"
	fmt.Printf("messages: %s\n", messagesPath)

	if err := runAgentLoop(ctx, model, computerTool, *flagPrompt, *flagMaxSteps, messagesPath); err != nil {
		return fmt.Errorf("agent loop: %w", err)
	}

	// Finalize.
	recording.stop()
	fmt.Printf("\nsaved recording: %s\n", recordingPath)

	openHostBrowser("file://" + recordingPath)
	fmt.Printf("opened recording: file://%s\n", recordingPath)
	return nil
}

func main() {
	if err := entrypoint(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
