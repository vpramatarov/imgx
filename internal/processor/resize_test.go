package processor

import (
	"image"
	"testing"
)

func TestResizeWidth(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 400, 200))
	got := Resize(src, ResizeSpec{Mode: ResizeWidth, Width: 200})
	if got.Bounds().Dx() != 200 || got.Bounds().Dy() != 100 {
		t.Errorf("ResizeWidth: got %dx%d, want 200x100", got.Bounds().Dx(), got.Bounds().Dy())
	}
}

func TestResizeHeight(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 400, 200))
	got := Resize(src, ResizeSpec{Mode: ResizeHeight, Height: 50})
	if got.Bounds().Dx() != 100 || got.Bounds().Dy() != 50 {
		t.Errorf("ResizeHeight: got %dx%d, want 100x50", got.Bounds().Dx(), got.Bounds().Dy())
	}
}

func TestResizeFitShrinks(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1000, 500))
	got := Resize(src, ResizeSpec{Mode: ResizeFit, Width: 200, Height: 200})
	if got.Bounds().Dx() > 200 || got.Bounds().Dy() > 200 {
		t.Errorf("ResizeFit: result exceeds bounding box: %dx%d", got.Bounds().Dx(), got.Bounds().Dy())
	}
}

func TestResizeFitNeverUpscales(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 100, 50))
	got := Resize(src, ResizeSpec{Mode: ResizeFit, Width: 1000, Height: 1000})
	if got.Bounds().Dx() != 100 || got.Bounds().Dy() != 50 {
		t.Errorf("ResizeFit upscaled: got %dx%d, want 100x50", got.Bounds().Dx(), got.Bounds().Dy())
	}
}

func TestResizeNonePassesThrough(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 10, 10))
	got := Resize(src, ResizeSpec{Mode: ResizeNone})
	if got != image.Image(src) {
		t.Error("ResizeNone should return the same image reference")
	}
}
