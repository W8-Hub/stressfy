package api

import (
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Router builds the chi router with all routes registered.
func (s *Server) Router() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", s.health)
	r.Get("/healthz", s.healthz)
	r.Get("/ready", s.ready)

	r.Post("/jobs", s.createJob)
	r.Get("/jobs", s.listJobs)
	r.Get("/jobs/{id}", s.getJob)
	r.Post("/jobs/{id}/stop", s.stopJob)

	r.Get("/net/source", s.netSource)
	r.Post("/net/sink", s.netSink)

	r.Get("/mock/status", s.mockStatus)
	r.Post("/mock/status", s.mockSetStatus)
	r.Get("/mock/error", s.mockError)
	r.Get("/mock/latency", s.mockLatency)

	r.Get("/docs", s.docs)
	r.Get("/openapi.yaml", s.openapiYAML)

	return r
}
