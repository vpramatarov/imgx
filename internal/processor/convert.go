package processor

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"io"

	// Register decoders for image.Decode that aren't pulled in by
	// named imports below. imaging already imports image/jpeg + image/png,
	// and the named import of tiff registers its decoder too.
	_ "image/jpeg"
	_ "image/png"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"

	"github.com/HugoSmits86/nativewebp"
	"github.com/disintegration/imaging"
	"golang.org/x/image/tiff"

	"github.com/vpramatarov/imgx/internal/format"
)

type EncodeOptions struct {
	Quality int // 1..100, applies to JPEG; 85 default
}

// Decode reads an image from r using any registered decoder. It also
// returns the detected format name (from Go's image package) for
// diagnostics.
func Decode(r io.Reader) (image.Image, string, error) {
	return image.Decode(r)
}

// Encode writes img to w in the requested format. Quality applies to
// JPEG and AVIF; pure-Go WebP encoding is lossless, so --quality is
// silently ignored for WebP.
//
// HEIC/AVIF support is gated behind the `heif` build tag; see
// convert_heif.go and convert_noheif.go.
func Encode(w io.Writer, img image.Image, f format.Format, opts EncodeOptions) error {
	q := opts.Quality
	if q < 1 || q > 100 {
		q = 85
	}
	switch f {
	case format.JPEG:
		// JPEG has no alpha channel. Go's stdlib JPEG encoder composites
		// transparent pixels against black, which produces black halos
		// around transparent-PNG logos — almost never what the user
		// expects. Flatten onto white (matches Photoshop/browser defaults)
		// when the source type carries alpha.
		if hasAlphaChannel(img) {
			img = flattenOnWhite(img)
		}
		return imaging.Encode(w, img, imaging.JPEG, imaging.JPEGQuality(q))
	case format.PNG:
		return imaging.Encode(w, img, imaging.PNG)
	case format.GIF:
		return gif.Encode(w, img, nil)
	case format.WebP:
		return nativewebp.Encode(w, img)
	case format.TIFF:
		return tiff.Encode(w, img, &tiff.Options{Compression: tiff.Deflate})
	case format.AVIF:
		return encodeAVIF(w, img, q)
	}
	return fmt.Errorf("no encoder for format %s", f)
}

// hasAlphaChannel gates the JPEG white-flatten step on source type, not
// on a pixel scan. An opaque *image.RGBA still returns true here — the
// flatten is a no-op against a solid image, and the type check avoids
// walking every pixel just to answer "could alpha exist?".
func hasAlphaChannel(img image.Image) bool {
	switch img.(type) {
	case *image.RGBA, *image.NRGBA, *image.RGBA64, *image.NRGBA64:
		return true
	}
	return false
}

// flattenOnWhite composites img over a white background and returns a
// fresh *image.RGBA. Used when encoding alpha-bearing sources as JPEG.
func flattenOnWhite(img image.Image) image.Image {
	b := img.Bounds()
	out := image.NewRGBA(b)
	draw.Draw(out, b, &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(out, b, img, b.Min, draw.Over)
	return out
}
