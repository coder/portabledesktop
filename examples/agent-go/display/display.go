package display

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"

	xdraw "golang.org/x/image/draw"
	"strconv"
	"strings"
)

const (
	// Anthropic recommended screenshot limits.
	MaxScreenshotLongEdge = 1568
	MaxScreenshotPixels   = 1_150_000
)

// Geometry holds native (actual VNC framebuffer) and declared
// (what we tell Anthropic) display dimensions.
type Geometry struct {
	NativeWidth    int
	NativeHeight   int
	DeclaredWidth  int
	DeclaredHeight int
}

// ComputeScaledSize returns the target dimensions for a screenshot,
// respecting Anthropic's recommended limits.
func ComputeScaledSize(w, h int) (int, int) {
	longEdge := float64(max(w, h))
	totalPx := float64(w * h)

	longEdgeScale := float64(MaxScreenshotLongEdge) / longEdge
	totalScale := math.Sqrt(float64(MaxScreenshotPixels) / totalPx)
	scale := math.Min(1, math.Min(longEdgeScale, totalScale))

	if scale >= 1 {
		return w, h
	}
	return max(1, int(math.Floor(float64(w)*scale))),
		max(1, int(math.Floor(float64(h)*scale)))
}

// MustGeometry is like NewGeometry but panics on error.
func MustGeometry(nativeWidth, nativeHeight int) Geometry {
	geometry, err := NewGeometry(nativeWidth, nativeHeight)
	if err != nil {
		panic(err)
	}
	return geometry
}

// NewGeometry creates a Geometry from native dimensions, computing
// the declared (scaled) dimensions automatically.
func NewGeometry(nativeWidth, nativeHeight int) (Geometry, error) {
	if nativeWidth <= 0 || nativeHeight <= 0 {
		return Geometry{}, fmt.Errorf(
			"native desktop geometry must be positive, got %dx%d",
			nativeWidth,
			nativeHeight,
		)
	}

	declaredWidth, declaredHeight := ComputeScaledSize(nativeWidth, nativeHeight)
	return Geometry{
		NativeWidth:    nativeWidth,
		NativeHeight:   nativeHeight,
		DeclaredWidth:  declaredWidth,
		DeclaredHeight: declaredHeight,
	}, nil
}

// ParseSessionGeometry parses a "WIDTHxHEIGHT" string and returns
// the corresponding Geometry.
func ParseSessionGeometry(raw string) (Geometry, error) {
	nativeWidth, nativeHeight, err := ParseGeometryString(raw)
	if err != nil {
		return Geometry{}, fmt.Errorf("parse session geometry %q: %w", raw, err)
	}
	return NewGeometry(nativeWidth, nativeHeight)
}

// ParseGeometryString parses a "WIDTHxHEIGHT" string into width and
// height integers.
func ParseGeometryString(raw string) (int, int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, 0, fmt.Errorf("geometry is empty")
	}

	separator := strings.IndexAny(raw, "xX")
	if separator <= 0 || separator == len(raw)-1 {
		return 0, 0, fmt.Errorf("geometry %q is not in WIDTHxHEIGHT form", raw)
	}

	width, err := strconv.Atoi(raw[:separator])
	if err != nil {
		return 0, 0, fmt.Errorf("parse width: %w", err)
	}

	rest := raw[separator+1:]
	heightEnd := 0
	for heightEnd < len(rest) && rest[heightEnd] >= '0' && rest[heightEnd] <= '9' {
		heightEnd++
	}
	if heightEnd == 0 {
		return 0, 0, fmt.Errorf("geometry %q is missing a numeric height", raw)
	}

	height, err := strconv.Atoi(rest[:heightEnd])
	if err != nil {
		return 0, 0, fmt.Errorf("parse height: %w", err)
	}
	if width <= 0 || height <= 0 {
		return 0, 0, fmt.Errorf("geometry must be positive, got %dx%d", width, height)
	}

	return width, height, nil
}

// NativeBounds returns the native framebuffer rectangle.
func (g Geometry) NativeBounds() image.Rectangle {
	return image.Rect(0, 0, g.NativeWidth, g.NativeHeight)
}

// DeclaredBounds returns the declared (scaled) display rectangle.
func (g Geometry) DeclaredBounds() image.Rectangle {
	return image.Rect(0, 0, g.DeclaredWidth, g.DeclaredHeight)
}

// IsZero reports whether the geometry has any zero dimension.
func (g Geometry) IsZero() bool {
	return g.NativeWidth == 0 || g.NativeHeight == 0 || g.DeclaredWidth == 0 || g.DeclaredHeight == 0
}

func scaleAxisDeclaredToNative(value, declaredSpan, nativeSpan int) float64 {
	if declaredSpan <= 0 || nativeSpan <= 0 {
		return 0
	}

	clamped := max(0, min(declaredSpan-1, value))
	return ((float64(clamped) + 0.5) * float64(nativeSpan) / float64(declaredSpan)) - 0.5
}

func scaleAxisNativeToDeclared(value, nativeSpan, declaredSpan int) float64 {
	if nativeSpan <= 0 || declaredSpan <= 0 {
		return 0
	}

	clamped := max(0, min(nativeSpan-1, value))
	return ((float64(clamped) + 0.5) * float64(declaredSpan) / float64(nativeSpan)) - 0.5
}

func clampPointToBounds(bounds image.Rectangle, point image.Point) image.Point {
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return bounds.Min
	}

	return image.Pt(
		max(bounds.Min.X, min(bounds.Max.X-1, point.X)),
		max(bounds.Min.Y, min(bounds.Max.Y-1, point.Y)),
	)
}

func clampRegionToBounds(bounds image.Rectangle, region image.Rectangle) image.Rectangle {
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return image.Rectangle{}
	}

	left := max(bounds.Min.X, min(bounds.Max.X-1, region.Min.X))
	top := max(bounds.Min.Y, min(bounds.Max.Y-1, region.Min.Y))
	right := max(left+1, min(bounds.Max.X, region.Max.X))
	bottom := max(top+1, min(bounds.Max.Y, region.Max.Y))
	return image.Rect(left, top, right, bottom)
}

// DeclaredPointToNative converts a point in declared (scaled)
// coordinates to native framebuffer coordinates.
func (g Geometry) DeclaredPointToNative(point image.Point) image.Point {
	native := image.Pt(
		int(math.Round(scaleAxisDeclaredToNative(point.X, g.DeclaredWidth, g.NativeWidth))),
		int(math.Round(scaleAxisDeclaredToNative(point.Y, g.DeclaredHeight, g.NativeHeight))),
	)
	return clampPointToBounds(g.NativeBounds(), native)
}

// NativePointToDeclared converts a point in native framebuffer
// coordinates to declared (scaled) coordinates.
func (g Geometry) NativePointToDeclared(point image.Point) image.Point {
	declared := image.Pt(
		int(math.Round(scaleAxisNativeToDeclared(point.X, g.NativeWidth, g.DeclaredWidth))),
		int(math.Round(scaleAxisNativeToDeclared(point.Y, g.NativeHeight, g.DeclaredHeight))),
	)
	return clampPointToBounds(g.DeclaredBounds(), declared)
}

// DeclaredRegionToNative converts a declared-coordinate region
// [x1, y1, x2, y2] (half-open) to a native framebuffer rectangle.
func (g Geometry) DeclaredRegionToNative(region [4]int) image.Rectangle {
	// The zoom region [x1, y1, x2, y2] uses half-open convention:
	// x2 and y2 are exclusive bounds, not inclusive pixel
	// coordinates. Convert the start point and the last included
	// pixel separately, then reconstruct the half-open native
	// rect.
	x1 := min(region[0], region[2])
	y1 := min(region[1], region[3])
	x2 := max(region[0], region[2])
	y2 := max(region[1], region[3])

	// Ensure at least a 1-pixel region in declared space.
	if x2 <= x1 {
		x2 = x1 + 1
	}
	if y2 <= y1 {
		y2 = y1 + 1
	}

	startNative := g.DeclaredPointToNative(image.Pt(x1, y1))
	// x2-1, y2-1 is the last included pixel in declared space.
	lastNative := g.DeclaredPointToNative(image.Pt(x2-1, y2-1))

	nativeRegion := image.Rect(
		startNative.X,
		startNative.Y,
		lastNative.X+1,
		lastNative.Y+1,
	)
	return clampRegionToBounds(g.NativeBounds(), nativeRegion)
}

// ParsePNGDimensions extracts width and height from the IHDR chunk
// of a PNG byte slice without fully decoding the image.
func ParsePNGDimensions(pngData []byte) (int, int, error) {
	if len(pngData) < 24 {
		return 0, 0, fmt.Errorf("png data is too short: %d bytes", len(pngData))
	}

	pngSignature := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	if !bytes.Equal(pngData[:8], pngSignature) {
		return 0, 0, fmt.Errorf("invalid png signature")
	}
	if string(pngData[12:16]) != "IHDR" {
		return 0, 0, fmt.Errorf("png missing IHDR chunk")
	}

	width := int(binary.BigEndian.Uint32(pngData[16:20]))
	height := int(binary.BigEndian.Uint32(pngData[20:24]))
	if width <= 0 || height <= 0 {
		return 0, 0, fmt.Errorf("invalid png dimensions %dx%d", width, height)
	}

	return width, height, nil
}

// CropDeclaredRegion decodes a full PNG screenshot, crops it to the
// given declared-coordinate region [x1, y1, x2, y2], and returns the
// cropped PNG bytes at the crop's natural size. This matches
// Anthropic's Python reference: no stretching or aspect-ratio
// distortion.
func CropDeclaredRegion(pngData []byte, region [4]int) ([]byte, error) {
	src, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return nil, fmt.Errorf("decode png: %w", err)
	}

	bounds := src.Bounds()
	x1 := max(bounds.Min.X, min(region[0], region[2]))
	y1 := max(bounds.Min.Y, min(region[1], region[3]))
	x2 := min(bounds.Max.X, max(region[0], region[2]))
	y2 := min(bounds.Max.Y, max(region[1], region[3]))
	if x2 <= x1 {
		x2 = x1 + 1
	}
	if y2 <= y1 {
		y2 = y1 + 1
	}
	cropRect := image.Rect(x1, y1, x2, y2)

	dst := image.NewRGBA(image.Rect(0, 0, cropRect.Dx(), cropRect.Dy()))
	draw.Draw(dst, dst.Bounds(), src, cropRect.Min, draw.Src)

	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, fmt.Errorf("encode cropped png: %w", err)
	}
	return buf.Bytes(), nil
}

// ScaleToTargetPixels scales a PNG image up so that the total
// pixel count is approximately targetPixels, maintaining the
// original aspect ratio. If the image already has at least
// targetPixels, it is returned unchanged.
func ScaleToTargetPixels(pngData []byte, targetPixels int) ([]byte, error) {
	src, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return nil, fmt.Errorf("decode png: %w", err)
	}

	bounds := src.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()
	currentPixels := srcW * srcH

	if currentPixels >= targetPixels {
		return pngData, nil
	}

	scale := math.Sqrt(float64(targetPixels) / float64(currentPixels))
	dstW := max(1, int(math.Round(float64(srcW)*scale)))
	dstH := max(1, int(math.Round(float64(srcH)*scale)))

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	xdraw.NearestNeighbor.Scale(dst, dst.Bounds(), src, bounds, xdraw.Over, nil)

	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, fmt.Errorf("encode scaled png: %w", err)
	}
	return buf.Bytes(), nil
}

// ValidateScreenshotDimensions checks that actual screenshot
// dimensions match the expected declared display size.
func ValidateScreenshotDimensions(actualWidth, actualHeight, wantWidth, wantHeight int) error {
	if actualWidth != wantWidth || actualHeight != wantHeight {
		return fmt.Errorf(
			"screenshot dimensions %dx%d do not match declared display %dx%d",
			actualWidth,
			actualHeight,
			wantWidth,
			wantHeight,
		)
	}
	return nil
}
