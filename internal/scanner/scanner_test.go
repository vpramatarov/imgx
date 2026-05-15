package scanner

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// tinyJPEG writes a 2x2 valid JPEG to path.
func tinyJPEG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func tinyPNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanSortAlpha(t *testing.T) {
	dir := t.TempDir()
	tinyJPEG(t, filepath.Join(dir, "b.jpg"))
	tinyJPEG(t, filepath.Join(dir, "a.jpg"))
	tinyPNG(t, filepath.Join(dir, "c.png"))
	// Non-image file must be skipped.
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Scan(dir, false, SortAlpha)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 images, got %d", len(got))
	}
	want := []string{"a.jpg", "b.jpg", "c.png"}
	for i, e := range got {
		if e.Name != want[i] {
			t.Errorf("position %d: got %q, want %q", i, e.Name, want[i])
		}
	}
}

func TestScanSortMTime(t *testing.T) {
	dir := t.TempDir()
	paths := []string{"first.jpg", "second.jpg", "third.jpg"}
	for i, p := range paths {
		full := filepath.Join(dir, p)
		tinyJPEG(t, full)
		// Stagger mtimes so ordering is deterministic.
		mt := time.Now().Add(time.Duration(i) * time.Second)
		if err := os.Chtimes(full, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Scan(dir, false, SortMTime)
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range got {
		if e.Name != paths[i] {
			t.Errorf("position %d: got %q, want %q", i, e.Name, paths[i])
		}
	}
}

func TestScanRecursive(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	tinyJPEG(t, filepath.Join(dir, "top.jpg"))
	tinyJPEG(t, filepath.Join(sub, "nested.jpg"))

	flat, _ := Scan(dir, false, SortAlpha)
	if len(flat) != 1 || flat[0].Name != "top.jpg" {
		t.Errorf("non-recursive scan should see 1 file; got %+v", flat)
	}
	rec, _ := Scan(dir, true, SortAlpha)
	if len(rec) != 2 {
		t.Errorf("recursive scan should see 2 files; got %d", len(rec))
	}
}

// ftypStub writes a minimal 12-byte ISO BMFF ftyp header with the given
// brand. The file is NOT a valid HEIC/AVIF — it has no mdat or iloc — but
// it is enough for the magic-byte scanner to identify the format.
func ftypStub(t *testing.T, path, brand string) {
	t.Helper()
	if len(brand) != 4 {
		t.Fatalf("brand must be 4 bytes, got %q", brand)
	}
	hdr := append([]byte{0, 0, 0, 0x20, 'f', 't', 'y', 'p'}, []byte(brand)...)
	if err := os.WriteFile(path, hdr, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanPicksHEICAndAVIF(t *testing.T) {
	// scanner uses magic-byte detection via format.Detect; as long as the
	// first 12 bytes match a known brand, CanDecode returns true and the
	// entry shows up. The scanner doesn't care whether libheif can actually
	// decode the body — that failure mode belongs to the pipeline.
	dir := t.TempDir()
	tinyJPEG(t, filepath.Join(dir, "a.jpg"))
	ftypStub(t, filepath.Join(dir, "b.heic"), "heic")
	ftypStub(t, filepath.Join(dir, "c.avif"), "avif")
	ftypStub(t, filepath.Join(dir, "d.heic"), "mif1")

	got, err := Scan(dir, false, SortAlpha)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		names := make([]string, 0, len(got))
		for _, e := range got {
			names = append(names, e.Name)
		}
		t.Fatalf("expected 4 entries (1 jpg + 2 heic + 1 avif), got %d: %v", len(got), names)
	}
}

func TestParseSortMode(t *testing.T) {
	for _, s := range []string{"", "none", "default", "alpha", "mtime"} {
		if _, err := ParseSortMode(s); err != nil {
			t.Errorf("ParseSortMode(%q) errored: %v", s, err)
		}
	}
	if _, err := ParseSortMode("bogus"); err == nil {
		t.Error("expected error on bogus sort mode")
	}
}
