//go:build heif

package processor

import (
	"bytes"
	"image"
	"testing"

	"github.com/vpramatarov/imgx/internal/format"
)

func TestEncodeAVIFRoundTrip(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 8, 8))
	var buf bytes.Buffer
	if err := Encode(&buf, src, format.AVIF, EncodeOptions{Quality: 80}); err != nil {
		t.Fatalf("encode avif: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("encoded AVIF is empty")
	}
	got, err := format.DetectReader(&buf)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if got != format.AVIF {
		t.Errorf("encoded AVIF, detected %s", got)
	}
}
