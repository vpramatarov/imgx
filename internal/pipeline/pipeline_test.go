package pipeline_test

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/vpramatarov/imgx/internal/config"
	"github.com/vpramatarov/imgx/internal/pipeline"
	"github.com/vpramatarov/imgx/internal/scanner"
)

func writePNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestRunKeepNames covers the CLI path, where entry names come raw from
// disk (spaces, uppercase extensions) rather than pre-cleaned by the
// server's saveUploads.
func TestRunKeepNames(t *testing.T) {
	inDir, outDir := t.TempDir(), t.TempDir()
	writePNG(t, filepath.Join(inDir, "My Photo.PNG"))
	writePNG(t, filepath.Join(inDir, "second.png"))

	mode, err := scanner.ParseSortMode("alpha")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := scanner.Scan(inDir, false, mode)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("scanned %d entries, want 2", len(entries))
	}

	cfg := config.Config{
		Input:       inDir,
		Output:      outDir,
		KeepNames:   true,
		Name:        "ignored",
		Format:      "jpg",
		Quality:     85,
		Concurrency: 2,
	}
	sum, err := pipeline.Run(context.Background(), cfg, entries)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if sum.Errors != 0 {
		t.Fatalf("run had %d errors: %v", sum.Errors, sum.Failed)
	}

	for _, name := range []string{"my-photo.jpg", "second.jpg"} {
		if _, err := os.Stat(filepath.Join(outDir, name)); err != nil {
			t.Errorf("missing expected output %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(outDir, "ignored-1.jpg")); err == nil {
		t.Errorf("base name was not ignored: ignored-1.jpg exists")
	}
}
