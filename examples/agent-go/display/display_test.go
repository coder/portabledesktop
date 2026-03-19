package display

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"image"
	"strings"
	"testing"
)

func TestParseSessionGeometry(t *testing.T) {
	t.Parallel()

	geometry, err := ParseSessionGeometry("1400x900")
	if err != nil {
		t.Fatalf("ParseSessionGeometry returned error: %v", err)
	}

	if geometry.NativeWidth != 1400 || geometry.NativeHeight != 900 {
		t.Fatalf("unexpected native geometry: %+v", geometry)
	}
	if geometry.DeclaredWidth != 1337 || geometry.DeclaredHeight != 859 {
		t.Fatalf("unexpected declared geometry: %+v", geometry)
	}
}

func TestParseGeometryStringRejectsInvalid(t *testing.T) {
	t.Parallel()

	_, _, err := ParseGeometryString("oops")
	if err == nil {
		t.Fatal("expected ParseGeometryString to fail")
	}
}

func TestDeclaredDisplaySizeDerivation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		nativeWidth    int
		nativeHeight   int
		declaredWidth  int
		declaredHeight int
	}{
		{name: "unchanged at 1280x800", nativeWidth: 1280, nativeHeight: 800, declaredWidth: 1280, declaredHeight: 800},
		{name: "scaled 1400x900", nativeWidth: 1400, nativeHeight: 900, declaredWidth: 1337, declaredHeight: 859},
		{name: "scaled 1920x1080", nativeWidth: 1920, nativeHeight: 1080, declaredWidth: 1429, declaredHeight: 804},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			geometry, err := NewGeometry(tc.nativeWidth, tc.nativeHeight)
			if err != nil {
				t.Fatalf("NewGeometry returned error: %v", err)
			}

			if geometry.DeclaredWidth != tc.declaredWidth || geometry.DeclaredHeight != tc.declaredHeight {
				t.Fatalf("declared geometry = %dx%d, want %dx%d", geometry.DeclaredWidth, geometry.DeclaredHeight, tc.declaredWidth, tc.declaredHeight)
			}
		})
	}
}

func TestDeclaredPointToNative(t *testing.T) {
	t.Parallel()

	geometry := MustGeometry(1920, 1080)

	tests := []struct {
		name     string
		declared image.Point
		want     image.Point
	}{
		{name: "origin", declared: image.Pt(0, 0), want: image.Pt(0, 0)},
		{name: "center", declared: image.Pt(714, 402), want: image.Pt(960, 540)},
		{name: "bottom right", declared: image.Pt(1428, 803), want: image.Pt(1919, 1079)},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := geometry.DeclaredPointToNative(tc.declared)
			if got != tc.want {
				t.Fatalf("DeclaredPointToNative(%v) = %v, want %v", tc.declared, got, tc.want)
			}
		})
	}
}

func TestNativePointToDeclared(t *testing.T) {
	t.Parallel()

	geometry := MustGeometry(1920, 1080)

	tests := []struct {
		name   string
		native image.Point
		want   image.Point
	}{
		{name: "origin", native: image.Pt(0, 0), want: image.Pt(0, 0)},
		{name: "center", native: image.Pt(960, 540), want: image.Pt(714, 402)},
		{name: "bottom right", native: image.Pt(1919, 1079), want: image.Pt(1428, 803)},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := geometry.NativePointToDeclared(tc.native)
			if got != tc.want {
				t.Fatalf("NativePointToDeclared(%v) = %v, want %v", tc.native, got, tc.want)
			}
		})
	}
}

func TestDeclaredRegionToNative(t *testing.T) {
	t.Parallel()

	geometry := MustGeometry(1400, 900)
	region := geometry.DeclaredRegionToNative([4]int{100, 200, 500, 600})
	want := image.Rect(105, 210, 524, 629)
	if region != want {
		t.Fatalf("DeclaredRegionToNative = %v, want %v", region, want)
	}
}

func TestClampPointToBounds(t *testing.T) {
	t.Parallel()

	bounds := image.Rect(0, 0, 1920, 1080)
	tests := []struct {
		name  string
		point image.Point
		want  image.Point
	}{
		{name: "left top", point: image.Pt(-10, -20), want: image.Pt(0, 0)},
		{name: "right bottom", point: image.Pt(3000, 2000), want: image.Pt(1919, 1079)},
		{name: "in bounds", point: image.Pt(500, 600), want: image.Pt(500, 600)},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := clampPointToBounds(bounds, tc.point)
			if got != tc.want {
				t.Fatalf("clampPointToBounds(%v) = %v, want %v", tc.point, got, tc.want)
			}
		})
	}
}

func TestClampRegionToBounds(t *testing.T) {
	t.Parallel()

	bounds := image.Rect(0, 0, 1920, 1080)
	region := clampRegionToBounds(bounds, image.Rect(-50, -25, 5000, 4000))
	want := image.Rect(0, 0, 1920, 1080)
	if region != want {
		t.Fatalf("clampRegionToBounds = %v, want %v", region, want)
	}
}

func TestOffByOneAtMaximumCoordinates(t *testing.T) {
	t.Parallel()

	geometry := MustGeometry(1400, 900)
	declaredMax := image.Pt(geometry.DeclaredWidth-1, geometry.DeclaredHeight-1)
	nativeMax := image.Pt(geometry.NativeWidth-1, geometry.NativeHeight-1)

	if got := geometry.DeclaredPointToNative(declaredMax); got != nativeMax {
		t.Fatalf("declared max mapped to %v, want %v", got, nativeMax)
	}
	if got := geometry.NativePointToDeclared(nativeMax); got != declaredMax {
		t.Fatalf("native max mapped to %v, want %v", got, declaredMax)
	}
}

func TestValidateScreenshotDimensions(t *testing.T) {
	t.Parallel()

	if err := ValidateScreenshotDimensions(1337, 859, 1337, 859); err != nil {
		t.Fatalf("expected dimensions to match: %v", err)
	}

	err := ValidateScreenshotDimensions(1337, 858, 1337, 859)
	if err == nil {
		t.Fatal("expected mismatched dimensions to fail")
	}
	if !strings.Contains(err.Error(), "do not match declared display") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParsePNGDimensions(t *testing.T) {
	t.Parallel()

	pngData := testPNG(320, 240)
	width, height, err := ParsePNGDimensions(pngData)
	if err != nil {
		t.Fatalf("ParsePNGDimensions returned error: %v", err)
	}
	if width != 320 || height != 240 {
		t.Fatalf("ParsePNGDimensions = %dx%d, want 320x240", width, height)
	}

	_, _, err = ParsePNGDimensions([]byte("not-a-png"))
	if err == nil {
		t.Fatal("expected invalid PNG to fail")
	}
}

func TestZoomRegionConversionIsStateless(t *testing.T) {
	t.Parallel()

	geometry := MustGeometry(1920, 1080)
	zoomRegion := [4]int{714, 402, 1428, 803}
	want := image.Rect(960, 540, 1918, 1078)

	got := geometry.DeclaredRegionToNative(zoomRegion)
	if got != want {
		t.Fatalf("DeclaredRegionToNative(%v) = %v, want %v", zoomRegion, got, want)
	}

	second := geometry.DeclaredRegionToNative(zoomRegion)
	if second != want {
		t.Fatalf("second DeclaredRegionToNative(%v) = %v, want %v", zoomRegion, second, want)
	}
}

func TestParseGeometryStringAcceptsOffsets(t *testing.T) {
	t.Parallel()

	width, height, err := ParseGeometryString("1400x900+0+0")
	if err != nil {
		t.Fatalf("ParseGeometryString returned error: %v", err)
	}
	if width != 1400 || height != 900 {
		t.Fatalf("ParseGeometryString = %dx%d, want 1400x900", width, height)
	}
}

func TestDeclaredToNativeToDeclaredRoundTripExhaustive(t *testing.T) {
	cases := []struct {
		name           string
		nativeWidth    int
		nativeHeight   int
		declaredWidth  int
		declaredHeight int
	}{
		{name: "scaled 1400x900", nativeWidth: 1400, nativeHeight: 900, declaredWidth: 1337, declaredHeight: 859},
		{name: "scaled 1920x1080", nativeWidth: 1920, nativeHeight: 1080, declaredWidth: 1429, declaredHeight: 804},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			geometry := MustGeometry(tc.nativeWidth, tc.nativeHeight)
			if geometry.DeclaredWidth != tc.declaredWidth || geometry.DeclaredHeight != tc.declaredHeight {
				t.Fatalf(
					"declared geometry = %dx%d, want %dx%d",
					geometry.DeclaredWidth,
					geometry.DeclaredHeight,
					tc.declaredWidth,
					tc.declaredHeight,
				)
			}

			for y := 0; y < geometry.DeclaredHeight; y++ {
				for x := 0; x < geometry.DeclaredWidth; x++ {
					declared := image.Pt(x, y)
					native := geometry.DeclaredPointToNative(declared)
					roundTrip := geometry.NativePointToDeclared(native)
					if roundTrip != declared {
						t.Fatalf(
							"declared %v -> native %v -> declared %v",
							declared,
							native,
							roundTrip,
						)
					}
				}
			}
		})
	}
}

func TestDeclaredRegionToNativeFullScreenBounds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		nativeWidth  int
		nativeHeight int
	}{
		{name: "scaled 1400x900", nativeWidth: 1400, nativeHeight: 900},
		{name: "scaled 1920x1080", nativeWidth: 1920, nativeHeight: 1080},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			geometry := MustGeometry(tc.nativeWidth, tc.nativeHeight)
			region := [4]int{0, 0, geometry.DeclaredWidth, geometry.DeclaredHeight}
			got := geometry.DeclaredRegionToNative(region)
			want := geometry.NativeBounds()
			if got != want {
				t.Fatalf("DeclaredRegionToNative(%v) = %v, want %v", region, got, want)
			}
		})
	}
}

func TestDeclaredRegionToNativeReversedMatchesForward(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		nativeWidth  int
		nativeHeight int
		forward      [4]int
		reversed     [4]int
		want         image.Rectangle
	}{
		{
			name:         "scaled 1400x900",
			nativeWidth:  1400,
			nativeHeight: 900,
			forward:      [4]int{100, 200, 500, 600},
			reversed:     [4]int{500, 600, 100, 200},
			want:         image.Rect(105, 210, 524, 629),
		},
		{
			name:         "scaled 1920x1080",
			nativeWidth:  1920,
			nativeHeight: 1080,
			forward:      [4]int{100, 200, 500, 600},
			reversed:     [4]int{500, 600, 100, 200},
			want:         image.Rect(135, 269, 672, 806),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			geometry := MustGeometry(tc.nativeWidth, tc.nativeHeight)
			forward := geometry.DeclaredRegionToNative(tc.forward)
			reversed := geometry.DeclaredRegionToNative(tc.reversed)
			if forward != reversed {
				t.Fatalf("forward %v != reversed %v", forward, reversed)
			}
			if forward != tc.want {
				t.Fatalf("DeclaredRegionToNative(%v) = %v, want %v", tc.forward, forward, tc.want)
			}
		})
	}
}

func TestDeclaredRegionToNativeSinglePixelAndThinRegions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		nativeWidth  int
		nativeHeight int
		region       [4]int
		want         image.Rectangle
	}{
		{
			name:         "1400x900 single pixel",
			nativeWidth:  1400,
			nativeHeight: 900,
			region:       [4]int{250, 300, 250, 300},
			want:         image.Rect(262, 314, 263, 315),
		},
		{
			name:         "1400x900 thin horizontal",
			nativeWidth:  1400,
			nativeHeight: 900,
			region:       [4]int{250, 300, 251, 300},
			want:         image.Rect(262, 314, 263, 315),
		},
		{
			name:         "1400x900 thin vertical",
			nativeWidth:  1400,
			nativeHeight: 900,
			region:       [4]int{250, 300, 250, 301},
			want:         image.Rect(262, 314, 263, 315),
		},
		{
			name:         "1920x1080 single pixel",
			nativeWidth:  1920,
			nativeHeight: 1080,
			region:       [4]int{250, 300, 250, 300},
			want:         image.Rect(336, 403, 337, 404),
		},
		{
			name:         "1920x1080 thin horizontal",
			nativeWidth:  1920,
			nativeHeight: 1080,
			region:       [4]int{250, 300, 251, 300},
			want:         image.Rect(336, 403, 337, 404),
		},
		{
			name:         "1920x1080 thin vertical",
			nativeWidth:  1920,
			nativeHeight: 1080,
			region:       [4]int{250, 300, 250, 301},
			want:         image.Rect(336, 403, 337, 404),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			geometry := MustGeometry(tc.nativeWidth, tc.nativeHeight)
			got := geometry.DeclaredRegionToNative(tc.region)
			if got != tc.want {
				t.Fatalf("DeclaredRegionToNative(%v) = %v, want %v", tc.region, got, tc.want)
			}
		})
	}
}

func TestDeclaredRegionToNativeClampsOutOfBounds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		nativeWidth  int
		nativeHeight int
		region       [4]int
		want         image.Rectangle
	}{
		{
			name:         "1400x900 full bounds when oversized",
			nativeWidth:  1400,
			nativeHeight: 900,
			region:       [4]int{-100, -50, 1437, 909},
			want:         image.Rect(0, 0, 1400, 900),
		},
		{
			name:         "1400x900 left edge clamp",
			nativeWidth:  1400,
			nativeHeight: 900,
			region:       [4]int{-100, 300, 100, 300},
			want:         image.Rect(0, 314, 105, 315),
		},
		{
			name:         "1400x900 bottom right clamp",
			nativeWidth:  1400,
			nativeHeight: 900,
			region:       [4]int{1387, 919, 1437, 1059},
			want:         image.Rect(1399, 899, 1400, 900),
		},
		{
			name:         "1920x1080 full bounds when oversized",
			nativeWidth:  1920,
			nativeHeight: 1080,
			region:       [4]int{-100, -50, 1529, 854},
			want:         image.Rect(0, 0, 1920, 1080),
		},
		{
			name:         "1920x1080 left edge clamp",
			nativeWidth:  1920,
			nativeHeight: 1080,
			region:       [4]int{-100, 300, 100, 300},
			want:         image.Rect(0, 403, 134, 404),
		},
		{
			name:         "1920x1080 bottom right clamp",
			nativeWidth:  1920,
			nativeHeight: 1080,
			region:       [4]int{1479, 864, 1529, 1004},
			want:         image.Rect(1919, 1079, 1920, 1080),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			geometry := MustGeometry(tc.nativeWidth, tc.nativeHeight)
			got := geometry.DeclaredRegionToNative(tc.region)
			if got != tc.want {
				t.Fatalf("DeclaredRegionToNative(%v) = %v, want %v", tc.region, got, tc.want)
			}
		})
	}
}

func TestParsePNGDimensionsFromBase64RoundTrip(t *testing.T) {
	t.Parallel()

	encoded := base64.StdEncoding.EncodeToString(testPNG(1280, 800))
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("DecodeString returned error: %v", err)
	}

	width, height, err := ParsePNGDimensions(decoded)
	if err != nil {
		t.Fatalf("ParsePNGDimensions returned error: %v", err)
	}
	if width != 1280 || height != 800 {
		t.Fatalf("ParsePNGDimensions = %dx%d, want 1280x800", width, height)
	}
}

func testPNG(width, height int) []byte {
	buf := bytes.NewBuffer(make([]byte, 0, 24))
	buf.Write([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})
	buf.Write([]byte{0x00, 0x00, 0x00, 0x0d})
	buf.WriteString("IHDR")
	_ = binary.Write(buf, binary.BigEndian, uint32(width))
	_ = binary.Write(buf, binary.BigEndian, uint32(height))
	buf.Write(make([]byte, 5))
	return buf.Bytes()
}
