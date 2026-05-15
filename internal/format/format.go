// Package format provides magic-byte detection and a registry of image
// formats that imgx knows how to read and/or write.
package format

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
)

type Format int

const (
	Unknown Format = iota
	JPEG
	PNG
	GIF
	WebP
	TIFF
	BMP
	AVIF
	HEIC
)

func (f Format) String() string {
	switch f {
	case JPEG:
		return "jpeg"
	case PNG:
		return "png"
	case GIF:
		return "gif"
	case WebP:
		return "webp"
	case TIFF:
		return "tiff"
	case BMP:
		return "bmp"
	case AVIF:
		return "avif"
	case HEIC:
		return "heic"
	default:
		return "unknown"
	}
}

// Ext returns the canonical file extension (no leading dot).
func (f Format) Ext() string {
	switch f {
	case JPEG:
		return "jpg"
	case PNG:
		return "png"
	case GIF:
		return "gif"
	case WebP:
		return "webp"
	case TIFF:
		return "tiff"
	case BMP:
		return "bmp"
	case AVIF:
		return "avif"
	case HEIC:
		return "heic"
	default:
		return ""
	}
}

// CanDecode reports whether imgx can read this format.
func (f Format) CanDecode() bool {
	switch f {
	case JPEG, PNG, GIF, WebP, TIFF, BMP, AVIF, HEIC:
		return true
	}
	return false
}

// CanEncode reports whether imgx can write this format.
//
// HEIC encoding is not offered — it typically requires libx265 (GPL) and
// is rarely the desired output. Use AVIF for modern HEIF-family output.
func (f Format) CanEncode() bool {
	switch f {
	case JPEG, PNG, GIF, WebP, TIFF, AVIF:
		return true
	}
	return false
}

// Parse parses a user-supplied format name. Accepts "jpg"/"jpeg",
// "tiff"/"tif", etc.
func Parse(s string) (Format, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "jpg", "jpeg":
		return JPEG, nil
	case "png":
		return PNG, nil
	case "gif":
		return GIF, nil
	case "webp":
		return WebP, nil
	case "tiff", "tif":
		return TIFF, nil
	case "bmp":
		return BMP, nil
	case "avif":
		return AVIF, nil
	case "heic", "heif":
		return HEIC, nil
	}
	return Unknown, fmt.Errorf("unsupported format %q", s)
}

// DetectReader reads the first few bytes of r to identify the format.
// It reads up to 16 bytes and returns Unknown if no signature matches.
func DetectReader(r io.Reader) (Format, error) {
	var hdr [16]byte
	n, err := io.ReadFull(r, hdr[:])
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return Unknown, err
	}
	return fromMagic(hdr[:n]), nil
}

// Detect opens path and identifies its image format from magic bytes.
func Detect(path string) (Format, error) {
	f, err := os.Open(path)
	if err != nil {
		return Unknown, err
	}
	defer f.Close()
	return DetectReader(f)
}

var (
	magicJPEG = []byte{0xFF, 0xD8, 0xFF}
	magicPNG  = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	magicGIF  = []byte{0x47, 0x49, 0x46, 0x38} // GIF87a and GIF89a
	magicBMP  = []byte{0x42, 0x4D}
	magicTIFL = []byte{0x49, 0x49, 0x2A, 0x00} // little-endian
	magicTIFB = []byte{0x4D, 0x4D, 0x00, 0x2A} // big-endian
	magicRIFF = []byte{'R', 'I', 'F', 'F'}
	magicWEBP = []byte{'W', 'E', 'B', 'P'}
	magicFTYP = []byte{'f', 't', 'y', 'p'}

	// ISO BMFF ftyp brands at offset 8.
	brandAVIF = []byte{'a', 'v', 'i', 'f'}
	brandAVIS = []byte{'a', 'v', 'i', 's'}
	brandHEIC = []byte{'h', 'e', 'i', 'c'}
	brandHEIX = []byte{'h', 'e', 'i', 'x'}
	brandMIF1 = []byte{'m', 'i', 'f', '1'}
	brandMSF1 = []byte{'m', 's', 'f', '1'}
)

func fromMagic(b []byte) Format {
	switch {
	case bytes.HasPrefix(b, magicJPEG):
		return JPEG
	case bytes.HasPrefix(b, magicPNG):
		return PNG
	case bytes.HasPrefix(b, magicGIF):
		return GIF
	case bytes.HasPrefix(b, magicBMP):
		return BMP
	case bytes.HasPrefix(b, magicTIFL), bytes.HasPrefix(b, magicTIFB):
		return TIFF
	case len(b) >= 12 && bytes.HasPrefix(b, magicRIFF) && bytes.Equal(b[8:12], magicWEBP):
		return WebP
	case len(b) >= 12 && bytes.Equal(b[4:8], magicFTYP):
		brand := b[8:12]
		switch {
		case bytes.Equal(brand, brandAVIF), bytes.Equal(brand, brandAVIS):
			return AVIF
		case bytes.Equal(brand, brandHEIC), bytes.Equal(brand, brandHEIX),
			bytes.Equal(brand, brandMIF1), bytes.Equal(brand, brandMSF1):
			return HEIC
		}
	}
	return Unknown
}
