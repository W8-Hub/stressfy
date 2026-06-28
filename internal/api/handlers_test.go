package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"stressfy/internal/config"
	"stressfy/internal/job"
)

func testServer() *Server {
	cfg := config.Config{
		DataDir:        "/tmp/stressfy-test",
		TZOffset:       "-03:00",
		MaxDurationSec: 900,
		MaxRAMPercent:  85,
		MaxDiskMB:      10240,
		MaxNetMB:       10240,
	}
	store := job.NewStore()
	// no-op runner: we test the HTTP contract, not the stress execution.
	return NewServer(cfg, store, func(*job.Job) {})
}

func do(t *testing.T, srv *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("content-type", "application/json")
	}
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, r)
	return w
}

func TestCreateJobReturns201(t *testing.T) {
	srv := testServer()
	// scheduled in the future so the no-op runner state stays predictable.
	w := do(t, srv, "POST", "/jobs", `{"cpu":50,"time":10,"startAt":"2030-01-01T00:00:00"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}
	var pub job.PublicJob
	if err := json.Unmarshal(w.Body.Bytes(), &pub); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if pub.ID == "" {
		t.Error("missing id")
	}
	if pub.Status != job.StatusScheduled {
		t.Errorf("status = %q, want scheduled", pub.Status)
	}
	if v, _ := pub.Request.CPUPercent.Val(); v != 50 {
		t.Errorf("echoed cpuPercent = %v, want 50", v)
	}
}

func TestGetJobNotFound(t *testing.T) {
	srv := testServer()
	w := do(t, srv, "GET", "/jobs/missing", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestCreateGetListStopFlow(t *testing.T) {
	srv := testServer()

	w := do(t, srv, "POST", "/jobs", `{"cpu":10,"time":10,"startAt":"2030-01-01T00:00:00"}`)
	var created job.PublicJob
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	// GET
	w = do(t, srv, "GET", "/jobs/"+created.ID, "")
	if w.Code != http.StatusOK {
		t.Fatalf("get status = %d", w.Code)
	}

	// LIST
	w = do(t, srv, "GET", "/jobs", "")
	var list []job.PublicJob
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
	}

	// STOP (scheduled → cancelled)
	w = do(t, srv, "POST", "/jobs/"+created.ID+"/stop", "")
	var stopped job.PublicJob
	_ = json.Unmarshal(w.Body.Bytes(), &stopped)
	if stopped.Status != job.StatusCancelled {
		t.Errorf("stopped status = %q, want cancelled", stopped.Status)
	}
}

func TestHealthEndpoints(t *testing.T) {
	srv := testServer()

	w := do(t, srv, "GET", "/healthz", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Errorf("healthz unexpected: %d %s", w.Code, w.Body.String())
	}

	w = do(t, srv, "GET", "/health", "")
	var health map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &health); err != nil {
		t.Fatalf("health decode: %v", err)
	}
	if health["service"] != "stress-api" {
		t.Errorf("service = %v, want stress-api", health["service"])
	}

	w = do(t, srv, "GET", "/ready", "")
	if w.Code != http.StatusOK {
		t.Errorf("ready status = %d", w.Code)
	}
}

func TestNetSourceContentLength(t *testing.T) {
	srv := testServer()
	w := do(t, srv, "GET", "/net/source?mb=2", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if got := w.Header().Get("content-length"); got != "2097152" {
		t.Errorf("content-length = %q, want 2097152", got)
	}
	if w.Body.Len() != 2097152 {
		t.Errorf("body len = %d, want 2097152", w.Body.Len())
	}
}

func TestNetSinkCountsBytes(t *testing.T) {
	srv := testServer()
	payload := strings.Repeat("x", 1024)
	w := do(t, srv, "POST", "/net/sink", payload)
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["receivedBytes"].(float64) != 1024 {
		t.Errorf("receivedBytes = %v, want 1024", resp["receivedBytes"])
	}
}
