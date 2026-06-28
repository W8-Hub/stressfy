package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestMockStatusDefault(t *testing.T) {
	srv := testServer()
	w := do(t, srv, "GET", "/mock/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["statusCode"].(float64) != 200 {
		t.Errorf("statusCode = %v, want 200", body["statusCode"])
	}
}

func TestMockSetStatusThenGet(t *testing.T) {
	srv := testServer()

	w := do(t, srv, "POST", "/mock/status", `{"statusCode":503}`)
	if w.Code != http.StatusOK {
		t.Fatalf("set status = %d, want 200", w.Code)
	}

	w = do(t, srv, "GET", "/mock/status", "")
	if w.Code != 503 {
		t.Errorf("GET /mock/status = %d, want 503", w.Code)
	}
}

func TestMockSetStatusInvalid(t *testing.T) {
	srv := testServer()
	for _, body := range []string{`{"statusCode":700}`, `{"statusCode":100}`, `{}`} {
		w := do(t, srv, "POST", "/mock/status", body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("POST %s = %d, want 400", body, w.Code)
		}
	}
}

func TestMockSetStatusAutoRevert(t *testing.T) {
	srv := testServer()
	// durationSec ~0.05s; the mocked code should revert to 200 shortly after.
	do(t, srv, "POST", "/mock/status", `{"statusCode":500,"durationSec":0.05}`)
	if w := do(t, srv, "GET", "/mock/status", ""); w.Code != 500 {
		t.Fatalf("immediately after set = %d, want 500", w.Code)
	}
	time.Sleep(150 * time.Millisecond)
	if w := do(t, srv, "GET", "/mock/status", ""); w.Code != 200 {
		t.Errorf("after revert = %d, want 200", w.Code)
	}
}

func TestMockErrorAlways5xx(t *testing.T) {
	srv := testServer()
	for i := 0; i < 10; i++ {
		w := do(t, srv, "GET", "/mock/error", "")
		if w.Code < 500 || w.Code > 599 {
			t.Errorf("req %d: status = %d, want 5xx", i, w.Code)
		}
	}
}

func TestMockLatency(t *testing.T) {
	srv := testServer()
	start := time.Now()
	w := do(t, srv, "GET", "/mock/latency?ms=60", "")
	elapsed := time.Since(start)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if elapsed < 60*time.Millisecond {
		t.Errorf("returned too fast: %v", elapsed)
	}
}

func TestMockLatencyClampedToCap(t *testing.T) {
	srv := testServer()
	srv.cfg.MaxLatencyMS = 30 // cap well below the requested delay
	start := time.Now()
	w := do(t, srv, "GET", "/mock/latency?ms=100000", "")
	elapsed := time.Since(start)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if elapsed > time.Second {
		t.Errorf("delay not clamped: %v", elapsed)
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["delayedMs"].(float64) != 30 {
		t.Errorf("delayedMs = %v, want 30 (clamped)", body["delayedMs"])
	}
}
