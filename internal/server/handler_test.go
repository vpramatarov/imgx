package server

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/image/bmp"
)

func defaultOpts() Options {
	return Options{
		Addr:            "127.0.0.1:0",
		MaxFileSize:     10 << 20,
		MaxFiles:        20,
		ShutdownTimeout: 5 * time.Second,
	}
}

// tinyPNG returns a valid PNG payload of the given dimensions.
func tinyPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 40), uint8(y * 40), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

type uploadFile struct {
	Field string
	Name  string
	Body  []byte
}

func buildMultipart(t *testing.T, files []uploadFile, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, f := range files {
		part, err := mw.CreateFormFile(f.Field, f.Name)
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := part.Write(f.Body); err != nil {
			t.Fatalf("write body: %v", err)
		}
	}
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatalf("write field %q: %v", k, err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return &buf, mw.FormDataContentType()
}

func TestIndexServesForm(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	newRouter(defaultOpts()).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `<form action="/process"`) {
		t.Fatalf("body missing <form action=\"/process\">:\n%s", rr.Body.String())
	}
}

func TestHealth(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	newRouter(defaultOpts()).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
}

func TestProcessHappyPath(t *testing.T) {
	body, ct := buildMultipart(t, []uploadFile{
		{Field: "files", Name: "one.png", Body: tinyPNG(t, 40, 40)},
		{Field: "files", Name: "two.png", Body: tinyPNG(t, 60, 30)},
	}, map[string]string{
		"name":        "batch",
		"resize-mode": "width",
		"width":       "20",
		"format":      "jpg",
		"quality":     "70",
	})
	req := httptest.NewRequest(http.MethodPost, "/process", body)
	req.Header.Set("Content-Type", ct)
	rr := httptest.NewRecorder()

	newRouter(defaultOpts()).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body:\n%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); got != "application/zip" {
		t.Fatalf("content-type = %q, want application/zip", got)
	}
	if !strings.Contains(rr.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("missing attachment disposition: %q", rr.Header().Get("Content-Disposition"))
	}

	zr, err := zip.NewReader(bytes.NewReader(rr.Body.Bytes()), int64(rr.Body.Len()))
	if err != nil {
		t.Fatalf("zip reader: %v", err)
	}
	if len(zr.File) != 2 {
		t.Fatalf("zip has %d entries, want 2: %v", len(zr.File), zipNames(zr))
	}
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, "batch-") || !strings.HasSuffix(f.Name, ".jpg") {
			t.Errorf("unexpected entry name: %q", f.Name)
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open zip entry: %v", err)
		}
		data, _ := io.ReadAll(rc)
		_ = rc.Close()
		if len(data) < 100 {
			t.Errorf("entry %q: only %d bytes, seems wrong", f.Name, len(data))
		}
	}
}

func TestProcessRejectsNoFiles(t *testing.T) {
	body, ct := buildMultipart(t, nil, map[string]string{"resize-mode": "none"})
	req := httptest.NewRequest(http.MethodPost, "/process", body)
	req.Header.Set("Content-Type", ct)
	rr := httptest.NewRecorder()
	newRouter(defaultOpts()).ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestProcessEnforcesTotalCap(t *testing.T) {
	opts := defaultOpts()
	opts.MaxFileSize = 1 << 10 // 1KB per file
	opts.MaxFiles = 2          // total cap = 2KB

	big := bytes.Repeat([]byte("A"), 5<<10) // 5KB — well over cap
	body, ct := buildMultipart(t, []uploadFile{
		{Field: "files", Name: "huge.png", Body: big},
	}, map[string]string{"resize-mode": "none"})
	req := httptest.NewRequest(http.MethodPost, "/process", body)
	req.Header.Set("Content-Type", ct)
	rr := httptest.NewRecorder()
	newRouter(opts).ServeHTTP(rr, req)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", rr.Code, rr.Body.String())
	}
}

func TestProcessEnforcesFileCount(t *testing.T) {
	opts := defaultOpts()
	opts.MaxFiles = 1
	body, ct := buildMultipart(t, []uploadFile{
		{Field: "files", Name: "a.png", Body: tinyPNG(t, 8, 8)},
		{Field: "files", Name: "b.png", Body: tinyPNG(t, 8, 8)},
	}, map[string]string{"resize-mode": "none"})
	req := httptest.NewRequest(http.MethodPost, "/process", body)
	req.Header.Set("Content-Type", ct)
	rr := httptest.NewRecorder()
	newRouter(opts).ServeHTTP(rr, req)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rr.Code)
	}
}

func TestProcessRejectsMissingWidthForWidthMode(t *testing.T) {
	body, ct := buildMultipart(t, []uploadFile{
		{Field: "files", Name: "x.png", Body: tinyPNG(t, 10, 10)},
	}, map[string]string{"resize-mode": "width"})
	req := httptest.NewRequest(http.MethodPost, "/process", body)
	req.Header.Set("Content-Type", ct)
	rr := httptest.NewRecorder()
	newRouter(defaultOpts()).ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestProcessRejectsNonImageUpload(t *testing.T) {
	body, ct := buildMultipart(t, []uploadFile{
		{Field: "files", Name: "note.txt", Body: []byte("hello, not an image")},
	}, map[string]string{"resize-mode": "none"})
	req := httptest.NewRequest(http.MethodPost, "/process", body)
	req.Header.Set("Content-Type", ct)
	rr := httptest.NewRecorder()
	newRouter(defaultOpts()).ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

// tinyBMP returns a valid BMP payload. BMP is decode-only in imgx
// (format.CanEncode excludes it), so it behaves like HEIC for the
// "Keep original" preflight without needing the heif build tag.
func tinyBMP(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 20), uint8(y * 20), 64, 255})
		}
	}
	var buf bytes.Buffer
	if err := bmp.Encode(&buf, img); err != nil {
		t.Fatalf("encode bmp: %v", err)
	}
	return buf.Bytes()
}

func TestProcessRejectsKeepOriginalForReadOnlyFormat(t *testing.T) {
	body, ct := buildMultipart(t, []uploadFile{
		{Field: "files", Name: "photo.bmp", Body: tinyBMP(t, 16, 16)},
	}, map[string]string{
		"resize-mode": "none",
		"format":      "", // Keep original — must be rejected for BMP input
	})
	req := httptest.NewRequest(http.MethodPost, "/process", body)
	req.Header.Set("Content-Type", ct)
	rr := httptest.NewRecorder()
	newRouter(defaultOpts()).ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
	got := rr.Body.String()
	// Message must name the file and the read-only format so the
	// user knows what to fix.
	if !strings.Contains(got, "photo.bmp") || !strings.Contains(strings.ToLower(got), "bmp") {
		t.Fatalf("error does not identify the offending file/format: %q", got)
	}
	if !strings.Contains(strings.ToLower(got), "jpeg") {
		t.Fatalf("error does not suggest writable formats: %q", got)
	}
}

func TestProcessPartialFailureEmitsErrorsManifest(t *testing.T) {
	// PNG succeeds, BMP fails because "Keep original" isn't in play
	// here — but BMP *can* be converted when format=jpg, so instead we
	// simulate a failure by mixing a corrupt PNG (valid magic, bad
	// stream) alongside a good one.
	corruptPNG := append([]byte(nil), tinyPNG(t, 10, 10)...)
	// Flip bytes in the middle of the IDAT stream to break decode while
	// keeping the PNG magic intact (so scanner still picks it up).
	for i := 40; i < 80 && i < len(corruptPNG); i++ {
		corruptPNG[i] ^= 0xFF
	}
	body, ct := buildMultipart(t, []uploadFile{
		{Field: "files", Name: "good.png", Body: tinyPNG(t, 20, 20)},
		{Field: "files", Name: "broken.png", Body: corruptPNG},
	}, map[string]string{
		"resize-mode": "none",
		"format":      "jpg",
	})
	req := httptest.NewRequest(http.MethodPost, "/process", body)
	req.Header.Set("Content-Type", ct)
	rr := httptest.NewRecorder()
	newRouter(defaultOpts()).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (partial success); body=%s", rr.Code, rr.Body.String())
	}
	zr, err := zip.NewReader(bytes.NewReader(rr.Body.Bytes()), int64(rr.Body.Len()))
	if err != nil {
		t.Fatalf("zip reader: %v", err)
	}
	var hasManifest, hasImage bool
	var manifestBody []byte
	for _, f := range zr.File {
		switch {
		case f.Name == "errors.txt":
			hasManifest = true
			rc, _ := f.Open()
			manifestBody, _ = io.ReadAll(rc)
			_ = rc.Close()
		case strings.HasSuffix(f.Name, ".jpg"):
			hasImage = true
		}
	}
	if !hasImage {
		t.Fatalf("zip missing the successful output: %v", zipNames(zr))
	}
	if !hasManifest {
		t.Fatalf("zip missing errors.txt for partial failure: %v", zipNames(zr))
	}
	if !strings.Contains(string(manifestBody), "broken.png") {
		t.Fatalf("errors.txt does not name the failed file: %q", manifestBody)
	}
}

func zipNames(zr *zip.Reader) []string {
	out := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		out = append(out, f.Name)
	}
	return out
}
