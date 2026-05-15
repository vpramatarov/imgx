package main

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/vpramatarov/imgx/internal/server"
)

func newServeCmd() *cobra.Command {
	var (
		addr            string
		maxFileSize     int64
		maxFiles        int
		shutdownTimeout time.Duration
	)
	cmd := &cobra.Command{
		Use:          "serve",
		Short:        "Run an HTTP server with a browser UI for batch processing",
		Long:         "Serves a minimal upload form at / and processes uploads into a streamed ZIP.\nUploads are processed in a per-request temp directory and never persisted.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return server.Run(ctx, server.Options{
				Addr:            addr,
				MaxFileSize:     maxFileSize,
				MaxFiles:        maxFiles,
				ShutdownTimeout: shutdownTimeout,
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&addr, "addr", "127.0.0.1:8080", "Address to listen on (use 0.0.0.0:PORT to expose on all interfaces)")
	f.Int64Var(&maxFileSize, "max-file-size", 50<<20, "Max size per uploaded file in bytes")
	f.IntVar(&maxFiles, "max-files", 100, "Max number of files per request")
	f.DurationVar(&shutdownTimeout, "shutdown-timeout", 30*time.Second, "How long to wait for in-flight requests on SIGINT/SIGTERM")
	return cmd
}
