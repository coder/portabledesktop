package desktop

import (
	"bytes"
	"fmt"
	"image/jpeg"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func intPtr(v int) *int { return &v }

func generateTestMP4(t *testing.T, width, height, durationSec int) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "test.mp4")
	cmd := exec.Command("ffmpeg",
		"-f", "lavfi",
		"-i", fmt.Sprintf("testsrc2=size=%dx%d:rate=1:duration=%d", width, height, durationSec),
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-y", out,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate test MP4: %v: %s", err, output)
	}
	return out
}

// Validation tests (no ffmpeg needed).

func TestThumbnail_BothWidthAndHeight(t *testing.T) {
	d := &Desktop{RuntimeDir: t.TempDir()}
	_, err := d.Thumbnail(ThumbnailOptions{
		InputFile: "dummy.mp4",
		Width:     intPtr(320),
		Height:    intPtr(240),
	})
	if err == nil {
		t.Fatalf("expected error when both Width and Height are set")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected error to contain %q, got: %v", "mutually exclusive", err)
	}
}

func TestThumbnail_ZeroWidth(t *testing.T) {
	d := &Desktop{RuntimeDir: t.TempDir()}
	_, err := d.Thumbnail(ThumbnailOptions{
		InputFile: "dummy.mp4",
		Width:     intPtr(0),
	})
	if err == nil {
		t.Fatalf("expected error for zero Width")
	}
	if !strings.Contains(err.Error(), "positive") {
		t.Fatalf("expected error to contain %q, got: %v", "positive", err)
	}
}

func TestThumbnail_NegativeWidth(t *testing.T) {
	d := &Desktop{RuntimeDir: t.TempDir()}
	_, err := d.Thumbnail(ThumbnailOptions{
		InputFile: "dummy.mp4",
		Width:     intPtr(-1),
	})
	if err == nil {
		t.Fatalf("expected error for negative Width")
	}
	if !strings.Contains(err.Error(), "positive") {
		t.Fatalf("expected error to contain %q, got: %v", "positive", err)
	}
}

func TestThumbnail_ZeroHeight(t *testing.T) {
	d := &Desktop{RuntimeDir: t.TempDir()}
	_, err := d.Thumbnail(ThumbnailOptions{
		InputFile: "dummy.mp4",
		Height:    intPtr(0),
	})
	if err == nil {
		t.Fatalf("expected error for zero Height")
	}
	if !strings.Contains(err.Error(), "positive") {
		t.Fatalf("expected error to contain %q, got: %v", "positive", err)
	}
}

func TestThumbnail_NegativeHeight(t *testing.T) {
	d := &Desktop{RuntimeDir: t.TempDir()}
	_, err := d.Thumbnail(ThumbnailOptions{
		InputFile: "dummy.mp4",
		Height:    intPtr(-1),
	})
	if err == nil {
		t.Fatalf("expected error for negative Height")
	}
	if !strings.Contains(err.Error(), "positive") {
		t.Fatalf("expected error to contain %q, got: %v", "positive", err)
	}
}

func TestThumbnail_EmptyInputFile(t *testing.T) {
	d := &Desktop{RuntimeDir: t.TempDir()}
	_, err := d.Thumbnail(ThumbnailOptions{
		InputFile: "",
	})
	if err == nil {
		t.Fatalf("expected error for empty InputFile")
	}
	if !strings.Contains(err.Error(), "input file") {
		t.Fatalf("expected error to contain %q, got: %v", "input file", err)
	}
}

// Integration tests (require ffmpeg).

func TestThumbnail_NativeResolution(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not found on PATH")
	}

	mp4 := generateTestMP4(t, 640, 480, 1)
	d := &Desktop{RuntimeDir: ""}
	data, err := d.Thumbnail(ThumbnailOptions{InputFile: mp4})
	if err != nil {
		t.Fatalf("Thumbnail: %v", err)
	}
	if len(data) < 3 || data[0] != 0xff || data[1] != 0xd8 || data[2] != 0xff {
		t.Fatalf("output does not start with JPEG magic bytes")
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode JPEG: %v", err)
	}
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	if w != 640 {
		t.Fatalf("expected width 640, got %d", w)
	}
	if h != 480 {
		t.Fatalf("expected height 480, got %d", h)
	}
}

func TestThumbnail_ScaleByWidth(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not found on PATH")
	}

	mp4 := generateTestMP4(t, 640, 480, 1)
	d := &Desktop{RuntimeDir: ""}
	data, err := d.Thumbnail(ThumbnailOptions{
		InputFile: mp4,
		Width:     intPtr(320),
	})
	if err != nil {
		t.Fatalf("Thumbnail: %v", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode JPEG: %v", err)
	}
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	if w != 320 {
		t.Fatalf("expected width 320, got %d", w)
	}
	if h != 240 {
		t.Fatalf("expected height 240, got %d", h)
	}
}

func TestThumbnail_ScaleByHeight(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not found on PATH")
	}

	mp4 := generateTestMP4(t, 640, 480, 1)
	d := &Desktop{RuntimeDir: ""}
	data, err := d.Thumbnail(ThumbnailOptions{
		InputFile: mp4,
		Height:    intPtr(120),
	})
	if err != nil {
		t.Fatalf("Thumbnail: %v", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode JPEG: %v", err)
	}
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	if h != 120 {
		t.Fatalf("expected height 120, got %d", h)
	}
	if w != 160 {
		t.Fatalf("expected width 160, got %d", w)
	}
}

func TestThumbnail_OddSourceDimension(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not found on PATH")
	}

	mp4 := generateTestMP4(t, 641, 481, 1)
	d := &Desktop{RuntimeDir: ""}
	data, err := d.Thumbnail(ThumbnailOptions{
		InputFile: mp4,
		Width:     intPtr(320),
	})
	if err != nil {
		t.Fatalf("Thumbnail: %v", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode JPEG: %v", err)
	}
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	if w != 320 {
		t.Fatalf("expected width 320, got %d", w)
	}
	if h%2 != 0 {
		t.Fatalf("expected even height, got %d", h)
	}
}

func TestThumbnail_MissingInputFile(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not found on PATH")
	}

	d := &Desktop{RuntimeDir: ""}
	_, err := d.Thumbnail(ThumbnailOptions{InputFile: "/nonexistent.mp4"})
	if err == nil {
		t.Fatalf("expected error for missing input file")
	}
}
