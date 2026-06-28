package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestOpenAPISpecServed(t *testing.T) {
	srv := testServer()
	w := do(t, srv, "GET", "/openapi.yaml", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("content-type"); !strings.Contains(ct, "yaml") {
		t.Errorf("content-type = %q, want yaml", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, "openapi: 3.0") {
		t.Error("spec does not look like OpenAPI 3.0")
	}
	// A couple of routes should be present in the spec.
	for _, want := range []string{"/jobs", "/mock/status", "/mock/latency"} {
		if !strings.Contains(body, want) {
			t.Errorf("spec missing path %q", want)
		}
	}
}

func TestDocsPageServed(t *testing.T) {
	srv := testServer()
	w := do(t, srv, "GET", "/docs", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("content-type"); !strings.Contains(ct, "text/html") {
		t.Errorf("content-type = %q, want html", ct)
	}
	if !strings.Contains(w.Body.String(), "swagger-ui") {
		t.Error("docs page missing swagger-ui")
	}
}
