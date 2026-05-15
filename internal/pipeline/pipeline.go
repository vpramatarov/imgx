// Package pipeline orchestrates per-image processing: decode, resize,
// re-encode (stripping EXIF), and write. It dispatches work across a
// pool of Concurrency workers but pre-assigns the output index {n} from
// the sorted entry list, so output filenames remain deterministic
// regardless of worker scheduling.
package pipeline

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/vpramatarov/imgx/internal/config"
	"github.com/vpramatarov/imgx/internal/format"
	"github.com/vpramatarov/imgx/internal/naming"
	"github.com/vpramatarov/imgx/internal/processor"
	"github.com/vpramatarov/imgx/internal/scanner"
)

type Summary struct {
	Processed int64
	Errors    int64
	Failed    []FailedFile
}

// FailedFile is a per-image failure surfaced to callers. Name is the
// input file's base name (not the temp path), Reason is the error text.
type FailedFile struct {
	Name   string
	Reason string
}

// Run processes all entries according to cfg. Per-image errors are
// logged and counted but never terminate the batch; a non-nil returned
// error means a setup problem (e.g. cannot create output directory).
func Run(ctx context.Context, cfg config.Config, entries []scanner.Entry) (Summary, error) {
	if err := os.MkdirAll(cfg.Output, 0o755); err != nil {
		return Summary{}, fmt.Errorf("create output dir: %w", err)
	}
	baseName := naming.Clean(cfg.Name)
	resolver := naming.NewResolver(cfg.Output)

	type job struct {
		entry scanner.Entry
		index int
	}
	jobs := make(chan job)

	var sum Summary
	var failedMu sync.Mutex
	var wg sync.WaitGroup
	workers := cfg.Concurrency
	if workers < 1 {
		workers = 1
	}
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := range jobs {
				if err := processOne(cfg, baseName, j.entry, j.index, resolver); err != nil {
					atomic.AddInt64(&sum.Errors, 1)
					log.Printf("warn: %s: %v", j.entry.Path, err)
					failedMu.Lock()
					sum.Failed = append(sum.Failed, FailedFile{Name: j.entry.Name, Reason: err.Error()})
					failedMu.Unlock()
					continue
				}
				atomic.AddInt64(&sum.Processed, 1)
			}
		}()
	}

	dispatch := func() {
		defer close(jobs)
		for i, e := range entries {
			select {
			case <-ctx.Done():
				return
			case jobs <- job{entry: e, index: i + 1}:
			}
		}
	}
	dispatch()
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return sum, err
	}
	return sum, nil
}

func processOne(cfg config.Config, baseName string, e scanner.Entry, index int, resolver *naming.Resolver) error {
	outFmt := e.Format
	if cfg.Format != "" {
		f, err := format.Parse(cfg.Format)
		if err != nil {
			return err
		}
		if !f.CanEncode() {
			return fmt.Errorf("cannot encode to %s in this build", f)
		}
		outFmt = f
	}
	if !outFmt.CanEncode() {
		return fmt.Errorf("no encoder for source format %s; pass --format to convert", outFmt)
	}

	in, err := os.Open(e.Path)
	if err != nil {
		return err
	}
	defer in.Close()
	img, _, err := image.Decode(in)
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	img = processor.Resize(img, resizeSpec(cfg))

	var buf bytes.Buffer
	if err := processor.Encode(&buf, img, outFmt, processor.EncodeOptions{Quality: cfg.Quality}); err != nil {
		return fmt.Errorf("encode: %w", err)
	}

	outPath, err := resolver.Resolve(baseName, index, outFmt.Ext())
	if err != nil {
		return fmt.Errorf("resolve: %w", err)
	}
	if cfg.DryRun {
		fmt.Printf("[dry-run] %s -> %s (%d bytes)\n", e.Path, outPath, buf.Len())
		return nil
	}
	tmp := outPath + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, outPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if cfg.Verbose {
		fmt.Printf("%s -> %s\n", filepath.Base(e.Path), filepath.Base(outPath))
	}
	return nil
}

func resizeSpec(cfg config.Config) processor.ResizeSpec {
	switch {
	case cfg.Width > 0:
		return processor.ResizeSpec{Mode: processor.ResizeWidth, Width: cfg.Width}
	case cfg.Height > 0:
		return processor.ResizeSpec{Mode: processor.ResizeHeight, Height: cfg.Height}
	case cfg.Fit[0] > 0 && cfg.Fit[1] > 0:
		return processor.ResizeSpec{Mode: processor.ResizeFit, Width: cfg.Fit[0], Height: cfg.Fit[1]}
	}
	return processor.ResizeSpec{Mode: processor.ResizeNone}
}
