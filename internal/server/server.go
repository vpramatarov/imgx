// Package server exposes imgx's batch pipeline over HTTP. Uploads land
// in a per-request temp dir, run through scanner + pipeline unchanged,
// and come back as a streamed ZIP. No state is kept between requests.
package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"
)

// Options holds the knobs exposed by the `imgx serve` subcommand.
type Options struct {
	Addr            string        // e.g. "127.0.0.1:8080"
	MaxFileSize     int64         // bytes, per uploaded file
	MaxFiles        int           // per request
	ShutdownTimeout time.Duration // how long Shutdown waits for in-flight requests
}

// Run starts the HTTP server and blocks until ctx is cancelled (SIGINT
// / SIGTERM via signal.NotifyContext in the caller) or the listener
// returns an error. On cancellation it invokes http.Server.Shutdown
// with the configured timeout, giving in-flight requests a chance to
// finish. Returns nil on clean shutdown.
func Run(ctx context.Context, opts Options) error {
	if opts.ShutdownTimeout <= 0 {
		opts.ShutdownTimeout = 30 * time.Second
	}
	srv := &http.Server{
		Addr:              opts.Addr,
		Handler:           newRouter(opts),
		ReadHeaderTimeout: 10 * time.Second,
		// No overall ReadTimeout / WriteTimeout: chi middleware.Timeout
		// scopes per-request, and large uploads can legitimately take
		// minutes on slow links.
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	log.Printf("imgx: listening on http://%s", opts.Addr)

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("listen: %w", err)
	case <-ctx.Done():
		log.Printf("imgx: shutting down (timeout %s)", opts.ShutdownTimeout)
		shutCtx, cancel := context.WithTimeout(context.Background(), opts.ShutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	}
}
