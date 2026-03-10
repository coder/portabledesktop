package desktop

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/coder/portabledesktop/pd/internal/runtime"
)

// ScreenshotOptions controls how a desktop screenshot is captured.
type ScreenshotOptions struct {
	// Region is an optional crop rectangle [x1, y1, x2, y2].
	Region *[4]int
	// ScaleToGeometry scales the cropped region back to the full
	// desktop geometry.
	ScaleToGeometry bool
	// TargetWidth and TargetHeight set a final resize. Both must be
	// provided together.
	TargetWidth  *int
	TargetHeight *int
	// TimeoutMs is the ffmpeg command timeout in milliseconds.
	TimeoutMs int
}

// Screenshot captures a PNG screenshot of the current display and
// returns the raw PNG bytes.
func (d *Desktop) Screenshot(opts ScreenshotOptions) ([]byte, error) {
	width, height, err := d.captureSize()
	if err != nil {
		return nil, fmt.Errorf("capture size: %w", err)
	}

	if opts.TargetWidth != nil && *opts.TargetWidth <= 0 {
		return nil, fmt.Errorf("target width must be positive, got %d", *opts.TargetWidth)
	}
	if opts.TargetHeight != nil && *opts.TargetHeight <= 0 {
		return nil, fmt.Errorf("target height must be positive, got %d", *opts.TargetHeight)
	}

	ffmpegBin := runtime.ResolveRuntimeBinary(d.RuntimeDir, "ffmpeg")

	args := []string{
		"-loglevel", "error",
		"-f", "x11grab",
		"-video_size", fmt.Sprintf("%dx%d", width, height),
		"-i", fmt.Sprintf(":%d", d.Display),
		"-frames:v", "1",
	}

	var filters []string
	outputWidth := width
	outputHeight := height

	if opts.Region != nil {
		r := opts.Region
		x1, y1, x2, y2 := r[0], r[1], r[2], r[3]
		left := max(0, min(x1, x2))
		top := max(0, min(y1, y2))
		right := max(left+1, min(width, max(x1, x2)))
		bottom := max(top+1, min(height, max(y1, y2)))
		cropW := right - left
		cropH := bottom - top
		filters = append(filters,
			fmt.Sprintf("crop=%d:%d:%d:%d", cropW, cropH, left, top),
		)
		outputWidth = cropW
		outputHeight = cropH

		if opts.ScaleToGeometry {
			filters = append(filters,
				fmt.Sprintf("scale=%d:%d:flags=neighbor", width, height),
			)
			outputWidth = width
			outputHeight = height
		}
	}

	if opts.TargetWidth != nil && opts.TargetHeight != nil {
		tw := *opts.TargetWidth
		th := *opts.TargetHeight
		if tw != outputWidth || th != outputHeight {
			filters = append(filters,
				fmt.Sprintf("scale=%d:%d", tw, th),
			)
		}
	}

	if len(filters) > 0 {
		args = append(args, "-vf", strings.Join(filters, ","))
	}

	args = append(args, "-f", "image2pipe", "-vcodec", "png", "pipe:1")

	ctx := context.Background()
	timeoutMs := opts.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 20000 // Default 20s timeout, matching TS.
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, ffmpegBin, args...)
	cmd.Env = d.Env()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf(
			"ffmpeg screenshot failed: %w: %s",
			err, strings.TrimSpace(stderr.String()),
		)
	}

	return stdout.Bytes(), nil
}

