package processor

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	"github.com/vpramatarov/imgx/internal/format"
)

func TestEncodeRoundTrip(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 8, 8))

	for _, f := range []format.Format{format.JPEG, format.PNG, format.GIF, format.TIFF, format.WebP} {
		f := f
		t.Run(f.String(), func(t *testing.T) {
			var buf bytes.Buffer
			if err := Encode(&buf, src, f, EncodeOptions{Quality: 80}); err != nil {
				t.Fatalf("encode %s: %v", f, err)
			}
			if buf.Len() == 0 {
				t.Errorf("encode %s produced no bytes", f)
			}
			// Sanity: the first bytes match the format's magic signature.
			got, err := format.DetectReader(&buf)
			if err != nil {
				t.Fatalf("detect: %v", err)
			}
			if got != f {
				t.Errorf("encoded %s, detected %s", f, got)
			}
		})
	}
}

func TestEncodeRejectsBMP(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 2))
	var buf bytes.Buffer
	if err := Encode(&buf, src, format.BMP, EncodeOptions{}); err == nil {
		t.Error("BMP is read-only; Encode should fail")
	}
}

// TestEncodeJPEGFlattensAlphaOntoWhite pins the PNG→JPEG behaviour:
// transparent source pixels must come out white, not Go's stdlib default
// of black. A 16×16 fully-transparent NRGBA covers more than one JPEG
// macroblock so we can safely sample the interior away from any padding
// or subsampling artifacts at the edges.
func TestEncodeJPEGFlattensAlphaOntoWhite(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: 0, G: 0, B: 0, A: 0})
		}
	}
	var buf bytes.Buffer
	if err := Encode(&buf, src, format.JPEG, EncodeOptions{Quality: 95}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := jpeg.Decode(&buf)
	if err != nil {
		t.Fatalf("decode jpeg: %v", err)
	}
	// Sample the interior — every pixel should decode near-white.
	// RGBA returns 16-bit values; 0xF000 ≈ 240/255 per channel, leaving
	// generous headroom for JPEG's lossy round-trip.
	r, g, b, _ := out.At(8, 8).RGBA()
	if r < 0xF000 || g < 0xF000 || b < 0xF000 {
		t.Errorf("transparent pixel flattened to (%d, %d, %d) — expected near-white (≥0xF000 per ch); would be near-black without the flatten", r>>8, g>>8, b>>8)
	}
}

func TestEncodeRejectsHEIC(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 2))
	var buf bytes.Buffer
	if err := Encode(&buf, src, format.HEIC, EncodeOptions{}); err == nil {
		t.Error("HEIC is read-only (AVIF is the HEIF-family output); Encode should fail")
	}
}
