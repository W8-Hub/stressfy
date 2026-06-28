package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseRequestMergesQueryAndBody(t *testing.T) {
	// cpu via query (string), time via body (number); body wins on conflicts.
	body := `{"time":4,"cpu":99}`
	r := httptest.NewRequest("POST", "/jobs?cpu=50&ram=30", strings.NewReader(body))
	r.Header.Set("content-type", "application/json")

	req, err := parseRequest(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// body cpu=99 should win over query cpu=50
	if v, _ := req.CPUPercent.Val(); v != 99 {
		t.Errorf("cpu = %v, want 99 (body wins)", v)
	}
	// ram only in query (string) → normalized to ramPercent
	if v, _ := req.RAMPercent.Val(); v != 30 {
		t.Errorf("ramPercent = %v, want 30 (from query alias)", v)
	}
	// time alias normalized to durationSec
	if v, _ := req.DurationSec.Val(); v != 4 {
		t.Errorf("durationSec = %v, want 4", v)
	}
}

func TestParseRequestQueryOnly(t *testing.T) {
	r := httptest.NewRequest("POST", "/jobs?cpu=50&time=10", nil)
	req, err := parseRequest(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, _ := req.CPUPercent.Val(); v != 50 {
		t.Errorf("cpu = %v, want 50", v)
	}
	if v, _ := req.DurationSec.Val(); v != 10 {
		t.Errorf("durationSec = %v, want 10", v)
	}
}

func TestParseRequestInvalidBodyIgnored(t *testing.T) {
	// non-object/invalid JSON body is ignored, query still applies.
	r := httptest.NewRequest("POST", "/jobs?cpu=25", strings.NewReader("not json"))
	req, err := parseRequest(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, _ := req.CPUPercent.Val(); v != 25 {
		t.Errorf("cpu = %v, want 25", v)
	}
}

func TestParseRequestNestedSpecs(t *testing.T) {
	body := `{"diskWrite":{"mb":128,"mbps":"200","fsync":true},"networkRead":{"url":"http://x/y"}}`
	r := httptest.NewRequest("POST", "/jobs", strings.NewReader(body))
	req, err := parseRequest(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.DiskWrite == nil {
		t.Fatal("diskWrite nil")
	}
	if v, _ := req.DiskWrite.MB.Val(); v != 128 {
		t.Errorf("diskWrite.mb = %v, want 128", v)
	}
	if v, _ := req.DiskWrite.MBps.Val(); v != 200 {
		t.Errorf("diskWrite.mbps = %v, want 200 (string coerced)", v)
	}
	if req.DiskWrite.Fsync == nil || !*req.DiskWrite.Fsync {
		t.Error("diskWrite.fsync not parsed")
	}
	if req.NetworkRead == nil || req.NetworkRead.URL != "http://x/y" {
		t.Error("networkRead.url not parsed")
	}
}
