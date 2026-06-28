package api

import (
	"encoding/json"
	"net/http"
	"time"

	"stressfy/internal/config"
	"stressfy/internal/job"
	"stressfy/internal/mock"
)

// Server wires together the HTTP handlers, the job store and configuration.
type Server struct {
	cfg       config.Config
	store     *job.Store
	startTime time.Time
	run       func(*job.Job)
	mock      *mock.Controller
}

// NewServer creates a Server. run is invoked when a scheduled job fires
// (typically stress.RunJob bound to the store and config).
func NewServer(cfg config.Config, store *job.Store, run func(*job.Job)) *Server {
	return &Server{
		cfg:       cfg,
		store:     store,
		startTime: time.Now(),
		run:       run,
		mock:      mock.NewController(),
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
