package main

import (
	"image"
	"testing"
)

func TestClickStatusMessageMatchesMarkerRectangles(t *testing.T) {
	root := image.Rect(0, 0, 1280, 800)

	for _, pos := range markerPositions(root) {
		want := pos.name + " click registered"
		rect := markerRect(pos)
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			for x := rect.Min.X; x < rect.Max.X; x++ {
				if got := clickStatusMessage(root, x, y); got != want {
					t.Fatalf("clickStatusMessage(%q marker at %d,%d) = %q, want %q", pos.name, x, y, got, want)
				}
			}
		}
	}
}

func TestClickStatusMessageIgnoresPointsOutsideMarkerRectangles(t *testing.T) {
	root := image.Rect(0, 0, 1280, 800)

	outsidePoints := []image.Point{
		image.Pt(defaultMarkerInset-1, defaultMarkerInset),
		image.Pt(defaultMarkerInset, defaultMarkerInset-1),
		image.Pt(root.Dx()-defaultMarkerInset-markerSize, defaultMarkerInset-1),
		image.Pt(root.Dx()-defaultMarkerInset, defaultMarkerInset),
		image.Pt(defaultMarkerInset-1, root.Dy()-defaultMarkerInset-markerSize),
		image.Pt(defaultMarkerInset, root.Dy()-defaultMarkerInset),
		image.Pt(root.Dx()-defaultMarkerInset-markerSize, root.Dy()-defaultMarkerInset),
		image.Pt(root.Dx()-defaultMarkerInset, root.Dy()-defaultMarkerInset-markerSize),
		image.Pt((root.Dx()-markerSize)/2-1, (root.Dy()-markerSize)/2),
		image.Pt((root.Dx()-markerSize)/2, (root.Dy()-markerSize)/2-1),
		image.Pt((root.Dx()-markerSize)/2+markerSize, (root.Dy()-markerSize)/2),
		image.Pt((root.Dx()-markerSize)/2, (root.Dy()-markerSize)/2+markerSize),
		image.Pt(0, 0),
		image.Pt(root.Dx()-1, root.Dy()-1),
	}

	for _, point := range outsidePoints {
		if got := clickStatusMessage(root, point.X, point.Y); got != "" {
			t.Fatalf("clickStatusMessage(%d, %d) = %q, want empty message", point.X, point.Y, got)
		}
	}
}
