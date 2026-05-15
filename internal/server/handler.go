package server

import (
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/vpramatarov/imgx/internal/format"
	"github.com/vpramatarov/imgx/internal/naming"
	"github.com/vpramatarov/imgx/internal/pipeline"
	"github.com/vpramatarov/imgx/internal/scanner"
)

// handleIndex renders the single-page form.
func handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := indexTmpl.Execute(w, nil); err != nil {
		log.Printf("index template: %v", err)
	}
}

// handleHealth is a tiny 200-OK endpoint for container health checks.
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "ok")
}

// makeProcessHandler returns the upload handler, closed over the server's
// Options. The handler:
//  1. Caps the request body at MaxFileSize * MaxFiles.
//  2. Parses multipart, enforces per-file size + file-count limits.
//  3. Writes each upload to a temp input dir (sanitising names).
//  4. Runs scanner + pipeline exactly like the CLI.
//  5. Streams a ZIP of the output dir back to the client.
//  6. Cleans up both temp dirs in deferreds.
func makeProcessHandler(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		totalCap := opts.MaxFileSize * int64(opts.MaxFiles)
		r.Body = http.MaxBytesReader(w, r.Body, totalCap)

		if err := r.ParseMultipartForm(32 << 20); err != nil {
			// MaxBytesReader-triggered errors surface here; map to 413.
			if _, ok := err.(*http.MaxBytesError); ok {
				httpError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("upload exceeds %d bytes total", totalCap))
				return
			}
			httpError(w, http.StatusBadRequest, "parse multipart: "+err.Error())
			return
		}
		defer func() {
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll()
			}
		}()

		in, err := parseInputs(r)
		if err != nil {
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
		if len(in.Files) > opts.MaxFiles {
			httpError(w, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("too many files: %d (max %d)", len(in.Files), opts.MaxFiles))
			return
		}
		for _, fh := range in.Files {
			if fh.Size > opts.MaxFileSize {
				httpError(w, http.StatusRequestEntityTooLarge,
					fmt.Sprintf("file %q is %d bytes (max %d per file)", fh.Filename, fh.Size, opts.MaxFileSize))
				return
			}
		}

		inDir, err := os.MkdirTemp("", "imgx-in-*")
		if err != nil {
			httpError(w, http.StatusInternalServerError, "mkdir input: "+err.Error())
			return
		}
		defer os.RemoveAll(inDir)
		outDir, err := os.MkdirTemp("", "imgx-out-*")
		if err != nil {
			httpError(w, http.StatusInternalServerError, "mkdir output: "+err.Error())
			return
		}
		defer os.RemoveAll(outDir)

		if err := saveUploads(in.Files, inDir); err != nil {
			httpError(w, http.StatusBadRequest, "save uploads: "+err.Error())
			return
		}

		cfg, err := in.toConfig(inDir, outDir, runtime.NumCPU())
		if err != nil {
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}

		mode, err := scanner.ParseSortMode(cfg.Sort)
		if err != nil {
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
		entries, err := scanner.Scan(cfg.Input, false, mode)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "scan: "+err.Error())
			return
		}
		if len(entries) == 0 {
			httpError(w, http.StatusBadRequest, "no decodable images in upload (magic-byte check failed)")
			return
		}

		// Preflight: when the user picks "Keep original", every uploaded
		// format must be one imgx can encode. HEIC and BMP are decode-only
		// (see format.CanEncode), so "keep original" + HEIC silently drops
		// every file at pipeline time. Catch it here with an actionable
		// message naming the offending files and writable formats.
		if cfg.Format == "" {
			if bad := readOnlyInputs(entries); len(bad) > 0 {
				httpError(w, http.StatusBadRequest, readOnlyMessage(bad))
				return
			}
		}

		summary, err := pipeline.Run(r.Context(), cfg, entries)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "pipeline: "+err.Error())
			return
		}
		if summary.Processed == 0 {
			msg := fmt.Sprintf("all %d image(s) failed to process", summary.Errors)
			if len(summary.Failed) > 0 {
				msg += fmt.Sprintf(" — first failure: %s: %s", summary.Failed[0].Name, summary.Failed[0].Reason)
			}
			httpError(w, http.StatusUnprocessableEntity, msg)
			return
		}
		// Partial failure: embed an errors.txt in the zip so the user can
		// see which files dropped out without having to read server logs.
		if len(summary.Failed) > 0 {
			writeErrorsManifest(outDir, summary.Failed)
		}

		filename := fmt.Sprintf("imgx-%s.zip", time.Now().UTC().Format("20060102-150405"))
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, filename))
		if err := streamZip(w, outDir); err != nil {
			log.Printf("stream zip: %v", err)
		}
	}
}

// readOnlyInputs returns entries whose source format imgx cannot re-encode.
// Called only when the user chose "Keep original" output.
func readOnlyInputs(entries []scanner.Entry) []scanner.Entry {
	var bad []scanner.Entry
	for _, e := range entries {
		if !e.Format.CanEncode() {
			bad = append(bad, e)
		}
	}
	return bad
}

func readOnlyMessage(bad []scanner.Entry) string {
	seen := make(map[format.Format]struct{}, len(bad))
	var formats []string
	var names []string
	for _, e := range bad {
		names = append(names, e.Name)
		if _, ok := seen[e.Format]; !ok {
			seen[e.Format] = struct{}{}
			formats = append(formats, strings.ToUpper(e.Format.String()))
		}
	}
	writable := "JPEG, PNG, WebP, AVIF, GIF, TIFF"
	return fmt.Sprintf("cannot keep original format: %d file(s) use read-only formats (%s): %s. Pick an output format (%s).",
		len(bad), strings.Join(formats, ", "), strings.Join(names, ", "), writable)
}

// writeErrorsManifest drops a plain-text summary of per-file failures
// into outDir before the zip is streamed. Errors are logged but not
// propagated — the caller's partial success still goes out.
func writeErrorsManifest(outDir string, failed []pipeline.FailedFile) {
	var b strings.Builder
	fmt.Fprintf(&b, "imgx: %d file(s) failed to process\n\n", len(failed))
	for _, f := range failed {
		fmt.Fprintf(&b, "%s: %s\n", f.Name, f.Reason)
	}
	if err := os.WriteFile(filepath.Join(outDir, "errors.txt"), []byte(b.String()), 0o644); err != nil {
		log.Printf("write errors manifest: %v", err)
	}
}

// saveUploads copies every multipart file part into destDir, giving each
// a sanitised basename. Collisions are resolved by appending -1, -2, ...
// filepath.Base is used as a first-line path-traversal guard.
func saveUploads(files []*multipart.FileHeader, destDir string) error {
	used := make(map[string]struct{}, len(files))
	for _, fh := range files {
		src, err := fh.Open()
		if err != nil {
			return fmt.Errorf("open %q: %w", fh.Filename, err)
		}
		base := filepath.Base(fh.Filename)
		stem, ext := splitExt(base)
		stem = naming.Clean(stem)
		if stem == "" {
			stem = "upload"
		}
		name := stem + ext
		for i := 1; ; i++ {
			if _, taken := used[name]; !taken {
				break
			}
			name = fmt.Sprintf("%s-%d%s", stem, i, ext)
		}
		used[name] = struct{}{}
		dst, err := os.Create(filepath.Join(destDir, name))
		if err != nil {
			_ = src.Close()
			return fmt.Errorf("create %q: %w", name, err)
		}
		_, err = io.Copy(dst, src)
		_ = src.Close()
		_ = dst.Close()
		if err != nil {
			return fmt.Errorf("write %q: %w", name, err)
		}
	}
	return nil
}

func splitExt(name string) (string, string) {
	ext := filepath.Ext(name)
	return name[:len(name)-len(ext)], ext
}

func httpError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	http.Error(w, msg, code)
}
