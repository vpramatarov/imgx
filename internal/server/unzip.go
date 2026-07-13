package server

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/vpramatarov/imgx/internal/format"
	"github.com/vpramatarov/imgx/internal/naming"
	"github.com/vpramatarov/imgx/internal/pipeline"
)

// maxZipEntries bounds central-directory processing and the size of the
// skip report; a legitimate batch never comes close.
const maxZipEntries = 10_000

// statusError carries an HTTP status through the upload-saving path so
// the handler can answer 413 for cap violations and 400 for bad archives.
type statusError struct {
	code int
	msg  string
}

func (e *statusError) Error() string { return e.msg }

// isZipUpload sniffs a multipart part via ReadAt, which does not move
// the read offset — the non-zip path can still io.Copy from byte 0.
// PK\x03\x04 = local file header; PK\x05\x06 = empty archive (EOCD only).
func isZipUpload(f multipart.File) bool {
	var hdr [4]byte
	if n, _ := f.ReadAt(hdr[:], 0); n < 4 {
		return false
	}
	if hdr[0] != 'P' || hdr[1] != 'K' {
		return false
	}
	return (hdr[2] == 3 && hdr[3] == 4) || (hdr[2] == 5 && hdr[3] == 6)
}

// junkZipEntry reports OS housekeeping entries (macOS resource forks,
// Finder/Explorer metadata) that are dropped without user-facing noise.
func junkZipEntry(name string) bool {
	slashed := strings.ReplaceAll(name, `\`, "/")
	if strings.HasPrefix(slashed, "__MACOSX/") {
		return true
	}
	base := path.Base(slashed)
	return base == ".DS_Store" || base == "Thumbs.db" || strings.HasPrefix(base, "._")
}

// reserveName cleans base into a unique file name within used — the same
// Clean/dedupe rules direct uploads get. used must be shared across all
// parts and archives of one request.
func reserveName(base string, used map[string]struct{}) string {
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
	return name
}

// extractZip expands one uploaded archive into destDir. Entries that sniff
// as decodable images are written through reserveName; everything else is
// skipped and reported. Caps mirror direct uploads: each image entry must
// be <= opts.MaxFileSize uncompressed, and the running image count
// (direct + extracted, `saved`) must stay <= opts.MaxFiles. Because the
// count is checked before every write and the size during every write,
// total bytes written can never exceed MaxFiles*MaxFileSize — no separate
// cumulative guard is needed.
func extractZip(src multipart.File, size int64, zipName, destDir string, used map[string]struct{}, opts Options, saved int) (int, []pipeline.FailedFile, error) {
	zr, err := zip.NewReader(src, size)
	if err != nil {
		return saved, nil, &statusError{http.StatusBadRequest,
			fmt.Sprintf("%s: not a valid zip archive: %v", zipName, err)}
	}
	if len(zr.File) > maxZipEntries {
		return saved, nil, &statusError{http.StatusRequestEntityTooLarge,
			fmt.Sprintf("%s contains %d entries (max %d); split the archive", zipName, len(zr.File), maxZipEntries)}
	}

	var skipped []pipeline.FailedFile
	sawEntry := false
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() || junkZipEntry(zf.Name) {
			continue
		}
		sawEntry = true
		display := zipName + "/" + zf.Name

		rc, oerr := zf.Open()
		if oerr != nil {
			log.Printf("warn: %s: entry %q: %v", zipName, zf.Name, oerr)
			skipped = append(skipped, pipeline.FailedFile{Name: display, Reason: "skipped: unreadable zip entry: " + oerr.Error()})
			continue
		}
		// One limited reader covers sniff + copy, so the cap counts the
		// sniffed bytes too. +1 lets us detect "over cap" vs "exactly cap".
		lr := io.LimitReader(rc, opts.MaxFileSize+1)
		var head [16]byte
		n, rerr := io.ReadFull(lr, head[:])
		if rerr != nil && rerr != io.EOF && rerr != io.ErrUnexpectedEOF {
			_ = rc.Close()
			log.Printf("warn: %s: entry %q: %v", zipName, zf.Name, rerr)
			skipped = append(skipped, pipeline.FailedFile{Name: display, Reason: "skipped: unreadable zip entry: " + rerr.Error()})
			continue
		}
		f, _ := format.DetectReader(bytes.NewReader(head[:n]))
		if !f.CanDecode() {
			_ = rc.Close()
			log.Printf("warn: %s: skipping entry %q: not a decodable image", zipName, zf.Name)
			skipped = append(skipped, pipeline.FailedFile{Name: display, Reason: "skipped: not a decodable image (zip entry)"})
			continue
		}
		// Fast reject on the header's declared size; the LimitReader below
		// re-enforces in case the header lies.
		if zf.UncompressedSize64 > uint64(opts.MaxFileSize) {
			_ = rc.Close()
			return saved, skipped, &statusError{http.StatusRequestEntityTooLarge, fmt.Sprintf(
				"zip entry %q in %s is %d bytes uncompressed (max %d per file); split the archive or run imgx serve with a larger --max-file-size",
				zf.Name, zipName, zf.UncompressedSize64, opts.MaxFileSize)}
		}
		if saved >= opts.MaxFiles {
			_ = rc.Close()
			return saved, skipped, &statusError{http.StatusRequestEntityTooLarge, fmt.Sprintf(
				"too many files: extracting %q from %s exceeds the %d-file limit; split the upload or run imgx serve with a larger --max-files",
				zf.Name, zipName, opts.MaxFiles)}
		}

		base := path.Base(strings.ReplaceAll(zf.Name, `\`, "/")) // zip-slip guard; naming.Clean neutralises leftovers
		name := reserveName(base, used)
		dstPath := filepath.Join(destDir, name)
		dst, cerr := os.Create(dstPath)
		if cerr != nil {
			_ = rc.Close()
			return saved, skipped, fmt.Errorf("create %q: %w", name, cerr)
		}
		_, werr := dst.Write(head[:n])
		var copied int64
		if werr == nil {
			copied, werr = io.Copy(dst, lr)
		}
		_ = rc.Close()
		_ = dst.Close()
		if werr != nil {
			_ = os.Remove(dstPath)
			var pe *fs.PathError
			if errors.As(werr, &pe) { // our disk failed — not the archive's fault
				return saved, skipped, fmt.Errorf("write %q: %w", name, werr)
			}
			log.Printf("warn: %s: entry %q: %v", zipName, zf.Name, werr)
			skipped = append(skipped, pipeline.FailedFile{Name: display, Reason: "skipped: unreadable zip entry: " + werr.Error()})
			continue
		}
		if int64(n)+copied > opts.MaxFileSize {
			_ = os.Remove(dstPath)
			return saved, skipped, &statusError{http.StatusRequestEntityTooLarge, fmt.Sprintf(
				"zip entry %q in %s exceeds %d bytes uncompressed (max per file); split the archive or run imgx serve with a larger --max-file-size",
				zf.Name, zipName, opts.MaxFileSize)}
		}
		saved++
	}
	if !sawEntry {
		skipped = append(skipped, pipeline.FailedFile{Name: zipName, Reason: "skipped: empty zip archive"})
	}
	return saved, skipped, nil
}
