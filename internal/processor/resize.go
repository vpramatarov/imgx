package processor

import (
	"image"

	"github.com/disintegration/imaging"
)

type ResizeMode int

const (
	ResizeNone ResizeMode = iota
	ResizeWidth
	ResizeHeight
	ResizeFit
)

type ResizeSpec struct {
	Mode   ResizeMode
	Width  int
	Height int
}

// Resize applies a resize spec and returns the transformed image. For
// ResizeFit it never upscales: images smaller than the bounding box are
// returned unchanged.
func Resize(img image.Image, s ResizeSpec) image.Image {
	switch s.Mode {
	case ResizeWidth:
		return imaging.Resize(img, s.Width, 0, imaging.Lanczos)
	case ResizeHeight:
		return imaging.Resize(img, 0, s.Height, imaging.Lanczos)
	case ResizeFit:
		b := img.Bounds()
		if b.Dx() <= s.Width && b.Dy() <= s.Height {
			return img
		}
		return imaging.Fit(img, s.Width, s.Height, imaging.Lanczos)
	}
	return img
}
