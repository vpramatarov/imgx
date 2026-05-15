//go:build !heif

// Stubs for non-heif builds. HEIC/AVIF decoding is not registered, so
// image.Decode will fail with "image: unknown format" on those inputs.
// AVIF encoding returns an explanatory error instead of being silently
// unavailable.
package processor

import (
	"errors"
	"image"
	"io"
)

var errNoHEIF = errors.New("HEIC/AVIF support requires the 'heif' build tag (needs libheif at build and runtime)")

func encodeAVIF(w io.Writer, img image.Image, quality int) error {
	return errNoHEIF
}
