package processor

import (
	"io"

	"github.com/rwcarlsen/goexif/exif"
)

// HasEXIF reports whether r's image carries decodable EXIF metadata.
//
// imgx strips EXIF by virtue of decoding to image.Image and re-encoding;
// this helper exists so tests can assert "output has no EXIF" against
// known inputs that do. Any decode error is treated as "no EXIF".
func HasEXIF(r io.Reader) bool {
	_, err := exif.Decode(r)
	return err == nil
}
