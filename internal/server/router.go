package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// newRouter wires routes + middleware. The Timeout applies to all
// requests; uploads that take longer than this will be cancelled via
// request context (which pipeline.Run respects).
func newRouter(opts Options) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(5 * time.Minute))

	r.Get("/", handleIndex)
	r.Get("/healthz", handleHealth)
	r.Post("/process", makeProcessHandler(opts))
	return r
}
