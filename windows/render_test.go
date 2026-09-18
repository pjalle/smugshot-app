package main

import (
	"image"
	"image/color"
	"strings"
	"testing"
	"time"
)

func solid(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	return img
}

func TestFullShrinksDimsAndOutlines(t *testing.T) {
	white := color.RGBA{255, 255, 255, 255}
	out, region := full(solid(4000, 2500, white), image.Rect(1000, 500, 2000, 1500))
	if out.Bounds().Dx() != 2000 || out.Bounds().Dy() != 1250 {
		t.Fatalf("size = %v, want 2000x1250", out.Bounds().Size())
	}
	if region != image.Rect(500, 250, 1000, 750) {
		t.Fatalf("region = %v", region)
	}
	if got := out.RGBAAt(750, 500); got != white {
		t.Errorf("inside the region must be untouched, got %v", got)
	}
	if got := out.RGBAAt(10, 10); got.R != 165 {
		t.Errorf("outside must be dimmed to 65%%, got %v", got)
	}
	if got := out.RGBAAt(498, 500); got != highlight {
		t.Errorf("the outline must sit just outside the region, got %v", got)
	}
}

func TestCropKeepsSharpnessAndMargin(t *testing.T) {
	out := crop(solid(3000, 2000, color.RGBA{10, 20, 30, 255}), image.Rect(1000, 1000, 1400, 1200))
	// margin = 15% of 400 = 60 on every side, no shrinking needed
	if out.Bounds().Dx() != 520 || out.Bounds().Dy() != 320 {
		t.Fatalf("size = %v, want 520x320", out.Bounds().Size())
	}
	if got := out.RGBAAt(58, 100); got != highlight {
		t.Errorf("outline expected at the region's left edge, got %v", got)
	}
	if got := out.RGBAAt(200, 150); got != (color.RGBA{10, 20, 30, 255}) {
		t.Errorf("pixels inside must be copied unchanged, got %v", got)
	}
}

func TestCropAtScreenEdgeDoesNotPanic(t *testing.T) {
	out := crop(solid(800, 600, color.RGBA{1, 2, 3, 255}), image.Rect(0, 0, 100, 50))
	if out.Bounds().Dx() != 140 || out.Bounds().Dy() != 90 {
		t.Fatalf("size = %v, want 140x90", out.Bounds().Size())
	}
}

func TestShotText(t *testing.T) {
	text := shotText(`C:\Users\you\.smugshots\x`, time.Date(2026, 9, 17, 21, 0, 0, 0, time.UTC), "brave.exe", "Quill", image.Rect(1, 2, 11, 22), image.Pt(2000, 1250))
	for _, want := range []string{"# Smugshot", "- App: brave.exe", "- Window: Quill", "x 1, y 2, w 10, h 20 of 2000x1250", `crop.png`} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q", want)
		}
	}
}
