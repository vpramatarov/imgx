package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/vpramatarov/imgx/internal/config"
	"github.com/vpramatarov/imgx/internal/pipeline"
	"github.com/vpramatarov/imgx/internal/scanner"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "imgx [flags] <input-dir>",
		Short: "Batch resize, rename, convert, and strip EXIF from images",
		Long: "imgx non-destructively processes images from <input-dir> into --out.",
		Args: cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: run,
	}
	f := cmd.Flags()
	f.StringP("out", "o", "", "Output directory (required)")
	f.StringP("name", "n", "", "Base name for output files (default: input folder name)")
	f.Bool("keep-names", false, "Keep each file's original name (sanitized); --name is ignored")
	f.IntP("width", "w", 0, "Target width in px (keeps aspect ratio)")
	f.Int("height", 0, "Target height in px (keeps aspect ratio)")
	f.String("fit", "", "Fit within bounding box WxH, e.g. 1200x1200")
	f.StringP("format", "f", "", "Output format: jpg, png, webp, gif, tiff (default: keep original)")
	f.IntP("quality", "q", 85, "JPEG quality 1-100 (WebP is lossless in this build)")
	f.String("sort", "", "Sort order: alpha, mtime (default: OS order)")
	f.BoolP("recursive", "r", false, "Process subdirectories recursively")
	f.StringP("config", "c", "", "Path to config file (YAML/TOML)")
	f.Bool("dry-run", false, "Print what would happen without writing files")
	f.BoolP("verbose", "v", false, "Verbose logging")
	f.Int("concurrency", 0, "Parallel workers (default: NumCPU)")
	cmd.AddCommand(newServeCmd())
	return cmd
}

func run(cmd *cobra.Command, args []string) error {
	start := time.Now()

	cfg, err := config.Load(cmd, args)
	if err != nil {
		return err
	}
	info, err := os.Stat(cfg.Input)
	if err != nil {
		return fmt.Errorf("input: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("input %q is not a directory", cfg.Input)
	}

	mode, err := scanner.ParseSortMode(cfg.Sort)
	if err != nil {
		return err
	}
	entries, err := scanner.Scan(cfg.Input, cfg.Recursive, mode)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}
	if len(entries) == 0 {
		fmt.Printf("No supported images found in %q\n", cfg.Input)
		return nil
	}
	if cfg.Verbose {
		fmt.Printf("Found %d image(s); workers=%d\n", len(entries), cfg.Concurrency)
	}

	summary, err := pipeline.Run(context.Background(), cfg, entries)
	if err != nil {
		return err
	}
	elapsed := time.Since(start)
	// Consolidated per-image failure list on stderr. The pipeline already
	// emits a warn: line as each one happens, but a grouped block at the
	// end is far easier to skim on big batches.
	if len(summary.Failed) > 0 {
		fmt.Fprintf(os.Stderr, "\nFailed (%d):\n", len(summary.Failed))
		for _, f := range summary.Failed {
			fmt.Fprintf(os.Stderr, "  %s: %s\n", f.Name, f.Reason)
		}
		fmt.Fprintln(os.Stderr)
	}
	line := fmt.Sprintf("Processed: %d  Errors: %d  Elapsed: %s  Dry-run: %v",
		summary.Processed, summary.Errors, humanDuration(elapsed), cfg.DryRun)
	if summary.Processed > 0 {
		avg := elapsed / time.Duration(summary.Processed)
		line += fmt.Sprintf("  (avg %s/image)", humanDuration(avg))
	}
	fmt.Println(line)
	if summary.Errors > 0 {
		os.Exit(2)
	}
	return nil
}

// humanDuration formats d into a concise, human-friendly string:
//
//	< 1s    -> "234ms"
//	< 1m    -> "4.2s"
//	< 1h    -> "2m 15s"
//	>= 1h   -> "1h 23m 45s"
func humanDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		ms := d.Milliseconds()
		if ms == 0 && d > 0 {
			return "<1ms"
		}
		return fmt.Sprintf("%dms", ms)
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d < time.Hour:
		m := int(d / time.Minute)
		s := int((d % time.Minute) / time.Second)
		return fmt.Sprintf("%dm %ds", m, s)
	default:
		h := int(d / time.Hour)
		m := int((d % time.Hour) / time.Minute)
		s := int((d % time.Minute) / time.Second)
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
}
