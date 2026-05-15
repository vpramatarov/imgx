package format

import (
	"bytes"
	"testing"
)

func TestFromMagic(t *testing.T) {
	// 12-byte headers that contain enough bytes for the longest signature
	// we test (RIFF/WEBP needs 12 bytes; ftypavif needs 12 bytes).
	cases := []struct {
		name string
		hdr  []byte
		want Format
	}{
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0, 0, 0, 0, 0, 0, 0}, JPEG},
		{"png", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0}, PNG},
		{"gif87", []byte{'G', 'I', 'F', '8', '7', 'a', 0, 0, 0, 0, 0, 0}, GIF},
		{"gif89", []byte{'G', 'I', 'F', '8', '9', 'a', 0, 0, 0, 0, 0, 0}, GIF},
		{"bmp", []byte{'B', 'M', 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, BMP},
		{"tiff-le", []byte{0x49, 0x49, 0x2A, 0x00, 0, 0, 0, 0, 0, 0, 0, 0}, TIFF},
		{"tiff-be", []byte{0x4D, 0x4D, 0x00, 0x2A, 0, 0, 0, 0, 0, 0, 0, 0}, TIFF},
		{"webp", []byte{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'W', 'E', 'B', 'P'}, WebP},
		{"avif", []byte{0, 0, 0, 0x20, 'f', 't', 'y', 'p', 'a', 'v', 'i', 'f'}, AVIF},
		{"avis", []byte{0, 0, 0, 0x20, 'f', 't', 'y', 'p', 'a', 'v', 'i', 's'}, AVIF},
		{"heic", []byte{0, 0, 0, 0x20, 'f', 't', 'y', 'p', 'h', 'e', 'i', 'c'}, HEIC},
		{"heix", []byte{0, 0, 0, 0x20, 'f', 't', 'y', 'p', 'h', 'e', 'i', 'x'}, HEIC},
		{"mif1", []byte{0, 0, 0, 0x20, 'f', 't', 'y', 'p', 'm', 'i', 'f', '1'}, HEIC},
		{"msf1", []byte{0, 0, 0, 0x20, 'f', 't', 'y', 'p', 'm', 's', 'f', '1'}, HEIC},
		{"unknown", []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}, Unknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fromMagic(tc.hdr); got != tc.want {
				t.Errorf("fromMagic(%s) = %v, want %v", tc.name, got, tc.want)
			}
			// Also test via DetectReader.
			got, err := DetectReader(bytes.NewReader(tc.hdr))
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("DetectReader(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestParse(t *testing.T) {
	cases := map[string]Format{
		"jpg":  JPEG,
		"jpeg": JPEG,
		"png":  PNG,
		"PNG":  PNG,
		"tif":  TIFF,
		"tiff": TIFF,
		"webp": WebP,
		"gif":  GIF,
		"bmp":  BMP,
		"avif": AVIF,
		"heic": HEIC,
		"heif": HEIC,
	}
	for in, want := range cases {
		got, err := Parse(in)
		if err != nil {
			t.Errorf("Parse(%q) errored: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("Parse(%q) = %v, want %v", in, got, want)
		}
	}
	if _, err := Parse("exr"); err == nil {
		t.Error("expected error for unknown format")
	}
}

func TestCapabilities(t *testing.T) {
	if !JPEG.CanDecode() || !JPEG.CanEncode() {
		t.Error("JPEG must be read+write")
	}
	if !BMP.CanDecode() || BMP.CanEncode() {
		t.Error("BMP is read-only")
	}
	if !AVIF.CanDecode() || !AVIF.CanEncode() {
		t.Error("AVIF must be read+write")
	}
	if !HEIC.CanDecode() {
		t.Error("HEIC must be readable")
	}
	if HEIC.CanEncode() {
		t.Error("HEIC is read-only (by design — AVIF is the HEIF-family output)")
	}
}
