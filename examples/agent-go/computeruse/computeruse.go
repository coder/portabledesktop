// Package computeruse provides shared building blocks for the
// example's provider-specific computer-use dispatchers.
//
// Each provider (Anthropic, OpenAI) parses tool calls into desktop CLI
// invocations and returns a screenshot. The Dispatcher interface lets
// main.go drive a provider-agnostic agent loop.
package computeruse

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"charm.land/fantasy"

	"github.com/coder/portabledesktop/examples/agent-go/desktop"
	"github.com/coder/portabledesktop/examples/agent-go/display"
	"github.com/coder/portabledesktop/examples/agent-go/reporting"
)

// DefaultScreenshotTimeoutMS is the default timeout the desktop
// screenshot CLI waits for a frame before failing.
const DefaultScreenshotTimeoutMS = 20000

// Config carries the runtime context every dispatcher needs.
//
// Geometry holds the active native and declared dimensions. The
// declared dimensions are what the model sees; native dimensions are
// what the X server uses. ScreenshotsDir, when non-empty, is used to
// persist captured frames for debugging.
type Config struct {
	Geometry            display.Geometry
	ScreenshotsDir      string
	ScreenshotTimeoutMS int
}

// ExecuteResult is the artifact produced by Dispatcher.Execute.
// Output carries the tool result content (typically a screenshot).
// ProviderOptions, when non-nil, is forwarded onto the
// fantasy.ToolResultPart so providers can read provider-specific tuning
// (for example OpenAI's `detail: "original"` flag on a
// computer_call_output).
type ExecuteResult struct {
	Output          fantasy.ToolResultOutputContent
	ProviderOptions fantasy.ProviderOptions
}

// Dispatcher abstracts a provider-specific computer-use tool. Each
// implementation registers a fantasy tool and parses incoming tool
// calls into desktop CLI invocations.
type Dispatcher interface {
	// Tool returns the fantasy.Tool to register with the model.
	Tool() fantasy.Tool
	// Execute runs every action carried by a single tool call and
	// returns the tool result, a human-readable label for logging,
	// and any execution error.
	Execute(
		ctx context.Context,
		call fantasy.ToolCallContent,
		metrics *reporting.AgentMetrics,
	) (ExecuteResult, string, error)
}

// TextResult builds a single-text-message tool result.
func TextResult(text string) []fantasy.ToolResultOutputContent {
	return []fantasy.ToolResultOutputContent{
		fantasy.ToolResultOutputContentText{Text: text},
	}
}

// ErrorResult builds a single-error tool result.
func ErrorResult(text string) []fantasy.ToolResultOutputContent {
	return []fantasy.ToolResultOutputContent{
		fantasy.ToolResultOutputContentError{Error: fmt.Errorf("%s", text)},
	}
}

// MoveMouseToDeclaredCoordinate converts a [x, y] declared-coordinate
// pair into native pixels via the active geometry and asks the desktop
// CLI to move the cursor there.
func MoveMouseToDeclaredCoordinate(
	cfg *Config,
	label string,
	coordinate [2]int64,
	metrics *reporting.AgentMetrics,
) error {
	return MoveMouseToDeclaredPoint(
		cfg,
		label,
		image.Pt(int(coordinate[0]), int(coordinate[1])),
		metrics,
	)
}

// MoveMouseToDeclaredPoint converts a declared-coordinate point to
// native pixels via the active geometry and asks the desktop CLI to
// move the cursor there.
func MoveMouseToDeclaredPoint(
	cfg *Config,
	label string,
	point image.Point,
	metrics *reporting.AgentMetrics,
) error {
	native := cfg.Geometry.DeclaredPointToNative(point)
	if err := desktop.ExecVoid(
		"mouse", "move",
		strconv.Itoa(native.X),
		strconv.Itoa(native.Y),
	); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	if metrics != nil {
		metrics.MouseMoves++
	}
	return nil
}

// CaptureScreenshotResult captures a fresh screenshot from the desktop
// CLI, validates its dimensions against the declared display, optionally
// crops/scales a zoom region, persists the frame to disk for debugging,
// and returns it as a fantasy media result. The returned slice is
// either a single media output on success or a single error/text
// output on failure; the second return is reserved for genuine I/O
// failures the caller should propagate.
func CaptureScreenshotResult(
	cfg *Config,
	metrics *reporting.AgentMetrics,
	region *[4]int,
) ([]fantasy.ToolResultOutputContent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("computeruse.Config is required")
	}

	timeout := cfg.ScreenshotTimeoutMS
	if timeout <= 0 {
		timeout = DefaultScreenshotTimeoutMS
	}
	targetWidth := cfg.Geometry.DeclaredWidth
	targetHeight := cfg.Geometry.DeclaredHeight

	// Always capture a full screenshot at the declared display size.
	args := []string{
		"screenshot",
		"--json",
		"--target-width", strconv.Itoa(targetWidth),
		"--target-height", strconv.Itoa(targetHeight),
		"--timeout-ms", strconv.Itoa(timeout),
	}

	out, err := desktop.Exec(args...)
	if err != nil {
		return ErrorResult(fmt.Sprintf("screenshot: %v", err)), nil
	}

	var result struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &result); err != nil {
		return ErrorResult(fmt.Sprintf("parse screenshot: %v", err)), nil
	}
	pngData, err := base64.StdEncoding.DecodeString(result.Data)
	if err != nil {
		return ErrorResult(fmt.Sprintf("invalid base64 screenshot: %v", err)), nil
	}
	actualWidth, actualHeight, err := display.ParsePNGDimensions(pngData)
	if err != nil {
		return ErrorResult(fmt.Sprintf("parse screenshot dimensions: %v", err)), nil
	}
	if err := display.ValidateScreenshotDimensions(
		actualWidth,
		actualHeight,
		cfg.Geometry.DeclaredWidth,
		cfg.Geometry.DeclaredHeight,
	); err != nil {
		fmt.Fprintf(os.Stderr, "    screenshot dimension mismatch: %v\n", err)
		return ErrorResult(err.Error()), nil
	}

	// For zoom: crop the declared-coordinate region from the full
	// screenshot, scale it up to ~1M pixels so the model gets a
	// detailed view, and return the result.
	if region != nil {
		pngData, err = display.CropDeclaredRegion(pngData, *region)
		if err != nil {
			return ErrorResult(fmt.Sprintf("crop zoom region: %v", err)), nil
		}
		pngData, err = display.ScaleToTargetPixels(pngData, 250_000)
		if err != nil {
			return ErrorResult(fmt.Sprintf("scale zoom screenshot: %v", err)), nil
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
			cfg.Geometry.DeclaredWidth,
			cfg.Geometry.DeclaredHeight,
		)
	}

	if metrics != nil {
		metrics.Screenshots++
	}

	if cfg.ScreenshotsDir != "" {
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
			filepath.Join(cfg.ScreenshotsDir, filename),
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

// MetadataToOptions converts a ProviderMetadata map into an equivalent
// ProviderOptions map. Both are []ProviderOptionsData maps keyed by
// the provider Name. Round-tripping the metadata back through the
// ToolCallPart options is required for the OpenAI computer use tool to
// replay computer_call output items via param.Override; for Anthropic
// it is harmless.
func MetadataToOptions(meta fantasy.ProviderMetadata) fantasy.ProviderOptions {
	if len(meta) == 0 {
		return nil
	}
	opts := make(fantasy.ProviderOptions, len(meta))
	for k, v := range meta {
		opts[k] = v
	}
	return opts
}
