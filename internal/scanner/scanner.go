// Package scanner walks an input directory and returns image files in a
// deterministic order. Files are recognised by magic bytes, not extension.
package scanner

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"time"

	"github.com/vpramatarov/imgx/internal/format"
)

type SortMode int

const (
	SortNone SortMode = iota // OS readdir / walk order
	SortAlpha
	SortMTime
)

func ParseSortMode(s string) (SortMode, error) {
	switch s {
	case "", "none", "default":
		return SortNone, nil
	case "alpha":
		return SortAlpha, nil
	case "mtime":
		return SortMTime, nil
	}
	return SortNone, fmt.Errorf("invalid sort mode %q (valid: alpha, mtime)", s)
}

type Entry struct {
	Path   string
	Name   string // base file name (no directory)
	Size   int64
	MTime  time.Time
	Format format.Format
}

// Scan walks root, optionally descending into subdirectories, and returns
// all image files recognised by internal/format. Unreadable files are
// silently skipped (they are reported by the pipeline later if needed).
func Scan(root string, recursive bool, mode SortMode) ([]Entry, error) {
	var entries []Entry
	walk := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if !recursive && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		f, derr := format.Detect(path)
		if derr != nil || !f.CanDecode() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		entries = append(entries, Entry{
			Path:   path,
			Name:   d.Name(),
			Size:   info.Size(),
			MTime:  info.ModTime(),
			Format: f,
		})
		return nil
	}
	if err := filepath.WalkDir(root, walk); err != nil {
		return nil, err
	}
	switch mode {
	case SortAlpha:
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	case SortMTime:
		sort.Slice(entries, func(i, j int) bool { return entries[i].MTime.Before(entries[j].MTime) })
	}
	return entries, nil
}
