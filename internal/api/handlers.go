package api

import (
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"stressfy/internal/config"
	"stressfy/internal/job"
)

const mb = 1024 * 1024

// createJob handles POST /jobs. It merges query+body, clamps the duration,
// resolves the start time and schedules the job.
func (s *Server) createJob(w http.ResponseWriter, r *http.Request) {
	req, err := parseRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}

	var durSrc any
	if req.DurationSec != nil {
		durSrc = float64(*req.DurationSec)
	}
	durationSec := config.ClampNumber(durSrc, 1, s.cfg.MaxDurationSec, 10)

	start := ""
	if req.StartAt != nil {
		start = *req.StartAt
	}
	startAt, err := s.cfg.ParseStartAt(start)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	duration := time.Duration(durationSec * float64(time.Second))
	j := s.store.Create(req, startAt, duration, s.run)

	writeJSON(w, http.StatusCreated, s.store.Public(j))
}

// listJobs handles GET /jobs.
func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	jobs := s.store.List()
	out := make([]job.PublicJob, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, s.store.Public(j))
	}
	writeJSON(w, http.StatusOK, out)
}

// getJob handles GET /jobs/:id.
func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	j, ok := s.store.Get(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, s.store.Public(j))
}

// stopJob handles POST /jobs/:id/stop.
func (s *Server) stopJob(w http.ResponseWriter, r *http.Request) {
	j, ok := s.store.Stop(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, s.store.Public(j))
}

// health handles GET /health with detailed status.
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	hostname, _ := os.Hostname()

	counts := map[job.JobStatus]int{}
	jobs := s.store.List()
	for _, j := range jobs {
		counts[s.store.Status(j)]++
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"status":    "healthy",
		"service":   "stress-api",
		"hostname":  hostname,
		"uptimeSec": int(time.Since(s.startTime).Seconds()),
		"now":       time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00"),
		"memory": map[string]float64{
			"rssMb":       round2(float64(rssBytes()) / mb),
			"heapUsedMb":  round2(float64(m.HeapInuse) / mb),
			"heapTotalMb": round2(float64(m.HeapSys) / mb),
		},
		"jobs": map[string]int{
			"total":     len(jobs),
			"scheduled": counts[job.StatusScheduled],
			"running":   counts[job.StatusRunning],
			"stopping":  counts[job.StatusStopping],
			"finished":  counts[job.StatusFinished],
			"failed":    counts[job.StatusFailed],
			"cancelled": counts[job.StatusCancelled],
		},
	})
}

// healthz handles GET /healthz (minimal liveness).
func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ready handles GET /ready (readiness).
func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	hostname, _ := os.Hostname()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"ready":     true,
		"hostname":  hostname,
		"uptimeSec": int(time.Since(s.startTime).Seconds()),
	})
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}

// rssBytes reads resident set size from /proc/self/statm (Linux); 0 elsewhere.
func rssBytes() int64 {
	raw, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(raw))
	if len(fields) < 2 {
		return 0
	}
	pages, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return 0
	}
	return pages * int64(os.Getpagesize())
}
