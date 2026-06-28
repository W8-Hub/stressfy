package api

import (
	"encoding/json"
	"io"
	"math/rand"
	"net/http"
	"time"

	"stressfy/internal/config"
)

// errorCodes is the set of 5xx codes /mock/error chooses from. The endpoint
// always returns one of these, so N requests yield N 5xx responses.
var errorCodes = []int{500, 502, 503, 504}

// mockStatus handles GET /mock/status — responds with the currently mocked
// status code and a JSON body describing it (and any pending swap).
func (s *Server) mockStatus(w http.ResponseWriter, r *http.Request) {
	state := s.mock.State()
	writeJSON(w, state.StatusCode, state)
}

type setStatusRequest struct {
	StatusCode  *int     `json:"statusCode"`
	StartAt     *string  `json:"startAt"`
	DurationSec *float64 `json:"durationSec"`
}

// mockSetStatus handles POST /mock/status — schedules a swap of the mocked
// status code, optionally at a future time and optionally auto-reverting to
// the default after durationSec.
func (s *Server) mockSetStatus(w http.ResponseWriter, r *http.Request) {
	var body setStatusRequest
	raw, _ := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err := json.Unmarshal(raw, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}

	if body.StatusCode == nil || *body.StatusCode < 200 || *body.StatusCode > 599 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_status_code"})
		return
	}

	start := ""
	if body.StartAt != nil {
		start = *body.StartAt
	}
	startAt, err := s.cfg.ParseStartAt(start)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	var revertAfter time.Duration
	if body.DurationSec != nil {
		sec := config.ClampNumber(*body.DurationSec, 0, s.cfg.MaxDurationSec, 0)
		revertAfter = time.Duration(sec * float64(time.Second))
	}

	s.mock.Schedule(*body.StatusCode, startAt, revertAfter)
	writeJSON(w, http.StatusOK, s.mock.State())
}

// mockError handles GET /mock/error — always responds with a random 5xx code.
func (s *Server) mockError(w http.ResponseWriter, r *http.Request) {
	code := errorCodes[rand.Intn(len(errorCodes))]
	writeJSON(w, code, map[string]any{
		"statusCode": code,
		"error":      "simulated_5xx",
	})
}

// mockLatency handles GET /mock/latency — waits for ?ms= (capped by
// MaxLatencyMS) then responds 200. Aborts early if the client disconnects.
func (s *Server) mockLatency(w http.ResponseWriter, r *http.Request) {
	ms := config.ClampNumber(r.URL.Query().Get("ms"), 0, s.cfg.MaxLatencyMS, 0)
	delay := time.Duration(ms) * time.Millisecond

	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"delayedMs": ms,
	})
}
