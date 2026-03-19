package clickprobe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/coder/portabledesktop/examples/agent-go/display"
)

const (
	BuildTimeout = 2 * time.Minute
	ReadyTimeout = 30 * time.Second
	PollInterval = 100 * time.Millisecond
	MarkerInset  = 24
	MarkerSize   = 4
	IdealClicks  = 5
	StatusSuffix = " click registered"
)

type Target struct {
	Name string
	Rect image.Rectangle
}

type Event struct {
	Type                string `json:"type"`
	Seq                 int    `json:"seq"`
	Button              int    `json:"button"`
	RootX               int    `json:"rootX"`
	RootY               int    `json:"rootY"`
	EventX              int    `json:"eventX"`
	EventY              int    `json:"eventY"`
	WindowX             int    `json:"windowX"`
	WindowY             int    `json:"windowY"`
	WindowWidth         int    `json:"windowWidth"`
	WindowHeight        int    `json:"windowHeight"`
	ScreenWidth         int    `json:"screenWidth"`
	ScreenHeight        int    `json:"screenHeight"`
	FullscreenRequested bool   `json:"fullscreenRequested"`
}

type OpenResult struct {
	PID     int      `json:"pid"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

type Session struct {
	BinaryPath string
	EventsFile string
	ReadyFile  string
	Launch     OpenResult
	Ready      *Event
}

type Analysis struct {
	ObservedClicks        int
	SuccessfulHits        int
	Misses                int
	UniqueRectangles      []string
	RepeatedRectangles    map[string]int
	MissingRectangles     []string
	HitOrder              []string
	AllRectanglesComplete bool
	IdealObservedClicks   bool
	IdealSuccessfulHits   bool
	IdealRun              bool
}

func Bounds(width, height int) image.Rectangle {
	return image.Rect(0, 0, width, height)
}

func Targets(bounds image.Rectangle) []Target {
	return []Target{
		{
			Name: "top-left",
			Rect: image.Rect(
				bounds.Min.X+MarkerInset,
				bounds.Min.Y+MarkerInset,
				bounds.Min.X+MarkerInset+MarkerSize,
				bounds.Min.Y+MarkerInset+MarkerSize,
			),
		},
		{
			Name: "top-right",
			Rect: image.Rect(
				bounds.Min.X+bounds.Dx()-MarkerInset-MarkerSize,
				bounds.Min.Y+MarkerInset,
				bounds.Min.X+bounds.Dx()-MarkerInset,
				bounds.Min.Y+MarkerInset+MarkerSize,
			),
		},
		{
			Name: "bottom-left",
			Rect: image.Rect(
				bounds.Min.X+MarkerInset,
				bounds.Min.Y+bounds.Dy()-MarkerInset-MarkerSize,
				bounds.Min.X+MarkerInset+MarkerSize,
				bounds.Min.Y+bounds.Dy()-MarkerInset,
			),
		},
		{
			Name: "bottom-right",
			Rect: image.Rect(
				bounds.Min.X+bounds.Dx()-MarkerInset-MarkerSize,
				bounds.Min.Y+bounds.Dy()-MarkerInset-MarkerSize,
				bounds.Min.X+bounds.Dx()-MarkerInset,
				bounds.Min.Y+bounds.Dy()-MarkerInset,
			),
		},
		{
			Name: "center",
			Rect: image.Rect(
				bounds.Min.X+(bounds.Dx()-MarkerSize)/2,
				bounds.Min.Y+(bounds.Dy()-MarkerSize)/2,
				bounds.Min.X+(bounds.Dx()-MarkerSize)/2+MarkerSize,
				bounds.Min.Y+(bounds.Dy()-MarkerSize)/2+MarkerSize,
			),
		},
	}
}

func TargetNames() []string {
	return []string{"top-left", "top-right", "bottom-left", "bottom-right", "center"}
}

func StatusMessage(bounds image.Rectangle, x, y int) string {
	point := image.Pt(x, y)
	for _, target := range Targets(bounds) {
		if point.In(target.Rect) {
			return target.Name + StatusSuffix
		}
	}
	return ""
}

func ClassifyEvent(bounds image.Rectangle, event Event) string {
	message := StatusMessage(bounds, event.RootX, event.RootY)
	if message == "" {
		return "miss"
	}
	return strings.TrimSuffix(message, StatusSuffix)
}

func repoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("resolve source path")
	}
	// clickprobe/ is one level deeper than agent-go/, so we need
	// three parent traversals to reach the repository root.
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..")), nil
}

func pdRoot() (string, error) {
	root, err := repoRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "pd"), nil
}

func BuildBinary(output string) error {
	pdModuleRoot, err := pdRoot()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return fmt.Errorf("create clickprobe bin dir: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), BuildTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "build", "-o", output, "./cmd/clickprobe")
	cmd.Dir = pdModuleRoot
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("go build timed out for clickprobe")
	}
	if err != nil {
		return fmt.Errorf(
			"build clickprobe: %w\nstdout:\n%s\nstderr:\n%s",
			err,
			stdout.String(),
			stderr.String(),
		)
	}
	return nil
}

// Start builds and launches the clickprobe binary inside the given
// session directory. pdExecFn is used to run portabledesktop CLI
// commands (e.g. "open"). markerSize sets the marker side length
// passed to the clickprobe binary.
func Start(sessionDir string, markerSize int, pdExecFn func(...string) (string, error)) (*Session, error) {
	probe := &Session{
		BinaryPath: filepath.Join(sessionDir, "bin", "clickprobe"),
		EventsFile: filepath.Join(sessionDir, "clickprobe.jsonl"),
		ReadyFile:  filepath.Join(sessionDir, "clickprobe.ready"),
	}
	_ = os.Remove(probe.EventsFile)
	_ = os.Remove(probe.ReadyFile)

	if err := BuildBinary(probe.BinaryPath); err != nil {
		return nil, err
	}

	out, err := pdExecFn(
		"open",
		"--",
		probe.BinaryPath,
		"--events-file", probe.EventsFile,
		"--ready-file", probe.ReadyFile,
		"--marker-size", fmt.Sprintf("%d", markerSize),
	)
	if err != nil {
		return nil, fmt.Errorf("launch clickprobe: %w", err)
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &probe.Launch); err != nil {
		return nil, fmt.Errorf("parse clickprobe launch result: %w", err)
	}
	if probe.Launch.PID == 0 {
		return nil, fmt.Errorf("clickprobe launch did not return a pid")
	}
	return probe, nil
}

func WaitForReady(ctx context.Context, probe *Session, expected image.Rectangle) (Event, error) {
	ticker := time.NewTicker(PollInterval)
	defer ticker.Stop()

	readyCtx, cancel := context.WithTimeout(ctx, ReadyTimeout)
	defer cancel()

	for {
		events, err := ReadEvents(probe.EventsFile)
		if err != nil {
			return Event{}, err
		}
		if ready, ok := findReadyEvent(events); ok {
			if err := validateReadyEvent(ready, expected); err != nil {
				return Event{}, err
			}
			if fileExists(probe.ReadyFile) {
				return ready, nil
			}
		}

		select {
		case <-readyCtx.Done():
			return Event{}, fmt.Errorf(
				"wait for clickprobe ready (pid=%d events=%s ready=%s): %w",
				probe.Launch.PID,
				probe.EventsFile,
				probe.ReadyFile,
				readyCtx.Err(),
			)
		case <-ticker.C:
		}
	}
}

func validateReadyEvent(ready Event, expected image.Rectangle) error {
	if ready.Type != "ready" {
		return fmt.Errorf("unexpected ready event type %q", ready.Type)
	}
	if ready.WindowX != 0 || ready.WindowY != 0 {
		return fmt.Errorf("clickprobe not fullscreen at origin: (%d,%d)", ready.WindowX, ready.WindowY)
	}
	if ready.WindowWidth != expected.Dx() || ready.WindowHeight != expected.Dy() {
		return fmt.Errorf(
			"unexpected clickprobe window size %dx%d; want %dx%d",
			ready.WindowWidth,
			ready.WindowHeight,
			expected.Dx(),
			expected.Dy(),
		)
	}
	if ready.ScreenWidth != expected.Dx() || ready.ScreenHeight != expected.Dy() {
		return fmt.Errorf(
			"unexpected clickprobe screen size %dx%d; want %dx%d",
			ready.ScreenWidth,
			ready.ScreenHeight,
			expected.Dx(),
			expected.Dy(),
		)
	}
	if !ready.FullscreenRequested {
		return fmt.Errorf("clickprobe did not report fullscreen requested")
	}
	return nil
}

func findReadyEvent(events []Event) (Event, bool) {
	for _, event := range events {
		if event.Type == "ready" {
			return event, true
		}
	}
	return Event{}, false
}

func ReadEvents(path string) ([]Event, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read clickprobe events %s: %w", path, err)
	}

	lines := strings.Split(string(data), "\n")
	events := make([]Event, 0, len(lines))
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var event Event
		err := json.Unmarshal([]byte(line), &event)
		if err != nil && i == len(lines)-1 && !bytes.HasSuffix(data, []byte("\n")) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse clickprobe event line %d from %s: %w", i+1, path, err)
		}
		events = append(events, event)
	}
	return events, nil
}

func AnalyzeEvents(path string, bounds image.Rectangle) (*Analysis, error) {
	events, err := ReadEvents(path)
	if err != nil {
		return nil, err
	}

	analysis := &Analysis{
		RepeatedRectangles: make(map[string]int),
	}
	seen := make(map[string]bool, len(TargetNames()))

	for _, event := range events {
		if event.Type != "click" {
			continue
		}
		analysis.ObservedClicks++
		classification := ClassifyEvent(bounds, event)
		analysis.HitOrder = append(analysis.HitOrder, classification)
		if classification == "miss" {
			analysis.Misses++
			continue
		}

		analysis.SuccessfulHits++
		if seen[classification] {
			analysis.RepeatedRectangles[classification]++
			continue
		}
		seen[classification] = true
		analysis.UniqueRectangles = append(analysis.UniqueRectangles, classification)
	}

	for _, name := range TargetNames() {
		if !seen[name] {
			analysis.MissingRectangles = append(analysis.MissingRectangles, name)
		}
	}

	analysis.AllRectanglesComplete = len(analysis.MissingRectangles) == 0
	analysis.IdealObservedClicks = analysis.ObservedClicks == IdealClicks
	analysis.IdealSuccessfulHits = analysis.SuccessfulHits == IdealClicks
	analysis.IdealRun = analysis.AllRectanglesComplete &&
		analysis.ObservedClicks == IdealClicks &&
		analysis.SuccessfulHits == IdealClicks &&
		analysis.Misses == 0 &&
		len(analysis.RepeatedRectangles) == 0

	return analysis, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Prompt is the default clickprobe prompt instructing the agent to click
// each orange rectangle in order.
const Prompt = "Click each orange rectangle in its exact center exactly once: top-left, top-right, bottom-left, bottom-right, and center. Use the screenshots returned after your actions to verify progress, confirm the matching '<name> click registered' banner before moving on, avoid unnecessary mouse movement, and stop as soon as all five unique rectangles are complete. The ideal run uses exactly 5 clicks."

// Runtime holds the live clickprobe session during an agent run.
type Runtime struct {
	Session *Session
}

// Summary holds the post-run clickprobe analysis.
type Summary struct {
	Session     *Session
	Analysis    *Analysis
	AnalysisErr error
}

// StartMode builds the clickprobe binary, launches it inside the given
// session directory, and waits until the probe reports ready.
func StartMode(
	ctx context.Context,
	sessionDir string,
	geometry *display.Geometry,
	pdExecFn func(...string) (string, error),
) (*Runtime, error) {
	fmt.Println("starting clickprobe...")
	probe, err := Start(sessionDir, MarkerSize, pdExecFn)
	if err != nil {
		return nil, fmt.Errorf("start clickprobe: %w", err)
	}
	fmt.Printf("clickprobe pid: %d\n", probe.Launch.PID)
	fmt.Printf("clickprobe events: %s\n", probe.EventsFile)

	ready, err := WaitForReady(ctx, probe, geometry.NativeBounds())
	if err != nil {
		return nil, fmt.Errorf("wait for clickprobe ready: %w", err)
	}
	probe.Ready = &ready

	resolvedGeometry, err := display.NewGeometry(ready.ScreenWidth, ready.ScreenHeight)
	if err != nil {
		return nil, fmt.Errorf("resolve ready geometry: %w", err)
	}
	*geometry = resolvedGeometry

	fmt.Printf(
		"clickprobe ready: window=%dx%d screen=%dx%d fullscreen=%s\n",
		ready.WindowWidth,
		ready.WindowHeight,
		ready.ScreenWidth,
		ready.ScreenHeight,
		boolStr(ready.FullscreenRequested),
	)
	fmt.Printf(
		"clickprobe active native geometry: %dx%d  declared geometry: %dx%d\n",
		geometry.NativeWidth,
		geometry.NativeHeight,
		geometry.DeclaredWidth,
		geometry.DeclaredHeight,
	)

	return &Runtime{Session: probe}, nil
}

// BuildSummary analyzes recorded clickprobe events and returns a
// summary. Returns nil when rt is nil.
func BuildSummary(rt *Runtime, geometry display.Geometry, defaultWidth, defaultHeight int) *Summary {
	if rt == nil || rt.Session == nil {
		return nil
	}

	probe := rt.Session
	summary := &Summary{Session: probe}
	bounds := geometry.NativeBounds()
	if bounds.Dx() == 0 || bounds.Dy() == 0 {
		bounds = Bounds(defaultWidth, defaultHeight)
	}
	if probe.Ready != nil {
		bounds = Bounds(probe.Ready.ScreenWidth, probe.Ready.ScreenHeight)
	}
	analysis, err := AnalyzeEvents(probe.EventsFile, bounds)
	if err != nil {
		summary.AnalysisErr = err
		return summary
	}
	summary.Analysis = analysis
	return summary
}

// FormatRepeatMap formats a target→count map for display.
func FormatRepeatMap(repeats map[string]int) string {
	if len(repeats) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(repeats))
	for _, name := range TargetNames() {
		count, ok := repeats[name]
		if !ok {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s(+%d)", name, count))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

// PrintSummary writes a human-readable clickprobe summary to stdout.
// agentClicks is the number of click actions the agent issued.
func PrintSummary(summary *Summary, agentClicks int) {
	fmt.Printf("  clickprobe events path: %s\n", summary.Session.EventsFile)
	fmt.Printf("  clickprobe ready file: %s\n", summary.Session.ReadyFile)
	fmt.Printf("  clickprobe pid: %d\n", summary.Session.Launch.PID)

	if summary.Analysis != nil {
		fmt.Printf("  clickprobe observed click events: %d\n", summary.Analysis.ObservedClicks)
		fmt.Printf("  clickprobe successful hits: %d\n", summary.Analysis.SuccessfulHits)
		fmt.Printf("  unique rectangles completed: %d (%s)\n", len(summary.Analysis.UniqueRectangles), formatList(summary.Analysis.UniqueRectangles))
		fmt.Printf("  misses: %d\n", summary.Analysis.Misses)
		fmt.Printf("  repeated rectangle clicks: %s\n", FormatRepeatMap(summary.Analysis.RepeatedRectangles))
		fmt.Printf("  missing rectangles: %s\n", formatList(summary.Analysis.MissingRectangles))
		fmt.Printf("  hit order: %s\n", formatList(summary.Analysis.HitOrder))
		fmt.Printf("  ideal-click target: %d\n", IdealClicks)
		fmt.Printf("  agent achieved ideal clicks: %s\n", boolStr(agentClicks == IdealClicks))
		fmt.Printf("  clickprobe observed ideal clicks: %s\n", boolStr(summary.Analysis.IdealObservedClicks))
		fmt.Printf("  task success: %s\n", boolStr(summary.Analysis.AllRectanglesComplete))
		fmt.Printf("  ideal run: %s\n", boolStr(summary.Analysis.IdealRun && agentClicks == IdealClicks))
		return
	}

	if summary.AnalysisErr != nil {
		fmt.Printf("  clickprobe analysis error: %v\n", summary.AnalysisErr)
	}
}

func boolStr(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func formatList(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}
