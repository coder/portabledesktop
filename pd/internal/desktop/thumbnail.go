package desktop

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/coder/portabledesktop/pd/internal/runtime"
)

// ThumbnailOptions controls how a JPEG thumbnail is extracted from
// an MP4 file.
type ThumbnailOptions struct {
	// InputFile is the path to the MP4 file.
	InputFile string
	// Width sets the output width in pixels. The height is
	// calculated automatically to preserve the aspect ratio.
	// Mutually exclusive with Height.
	Width *int
	// Height sets the output height in pixels. The width is
	// calculated automatically to preserve the aspect ratio.
	// Mutually exclusive with Width.
	Height *int
}

// Thumbnail extracts a single JPEG frame from the given MP4 file
// and returns the raw JPEG bytes.
func (d *Desktop) Thumbnail(opts ThumbnailOptions) ([]byte, error) {
	if opts.InputFile == "" {
		return nil, fmt.Errorf("input file is required")
	}
	if opts.Width != nil && opts.Height != nil {
		return nil, fmt.Errorf("width and height are mutually exclusive")
	}
	if opts.Width != nil && *opts.Width <= 0 {
		return nil, fmt.Errorf("width must be positive, got %d", *opts.Width)
	}
	if opts.Height != nil && *opts.Height <= 0 {
		return nil, fmt.Errorf("height must be positive, got %d", *opts.Height)
	}

	ffmpegBin := runtime.ResolveRuntimeBinary(d.RuntimeDir, "ffmpeg")

	args := []string{
		"-loglevel", "error",
		"-i", opts.InputFile,
		"-frames:v", "1",
	}

	if opts.Width != nil {
		args = append(args, "-vf", fmt.Sprintf("scale=%d:-2", *opts.Width))
	} else if opts.Height != nil {
		args = append(args, "-vf", fmt.Sprintf("scale=-2:%d", *opts.Height))
	}

	args = append(args, "-f", "image2pipe", "-vcodec", "mjpeg", "-q:v", "2", "pipe:1")

	cmd := exec.Command(ffmpegBin, args...)
	cmd.Env = d.Env()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf(
			"ffmpeg thumbnail failed: %w: %s",
			err, strings.TrimSpace(stderr.String()),
		)
	}

	if stdout.Len() == 0 {
		return nil, fmt.Errorf("no frame extracted")
	}

	return stdout.Bytes(), nil
}
