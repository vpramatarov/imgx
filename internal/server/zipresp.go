package server

import (
	"archive/zip"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// streamZip walks dir and writes every regular file into w as a zip
// entry named by the file's basename. The zip is written with Store
// (no compression) because image formats are already compressed —
// deflate burns CPU for a sub-percent size reduction.
func streamZip(w io.Writer, dir string) error {
	zw := zip.NewWriter(w)
	defer zw.Close()

	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		hdr, herr := zip.FileInfoHeader(info)
		if herr != nil {
			return herr
		}
		hdr.Name = filepath.Base(path)
		hdr.Method = zip.Store
		entry, werr := zw.CreateHeader(hdr)
		if werr != nil {
			return werr
		}
		f, oerr := os.Open(path)
		if oerr != nil {
			return oerr
		}
		_, cerr := io.Copy(entry, f)
		_ = f.Close()
		return cerr
	})
}
