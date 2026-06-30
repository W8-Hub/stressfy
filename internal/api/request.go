package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"stressfy/internal/job"
)

// maxBodyBytes caps how much of the request body we read when parsing job specs.
const maxBodyBytes = 1 << 20 // 1 MiB

// errInvalidBody signals that a request body was present but not a valid JSON
// object. The handler turns it into a 400 so a malformed request fails loudly
// instead of silently running with empty parameters.
var errInvalidBody = errors.New("invalid_json_body")

// parseRequest builds a StressRequest by merging query parameters and the JSON
// body, with the body taking precedence — mirroring the original `{...query,
// ...body}` behavior. Query values arrive as strings and are tolerated by the
// Number type. An empty body is fine (query/defaults are used), but a body that
// is present and cannot be parsed as a JSON object returns errInvalidBody so the
// caller is told instead of having the body silently dropped.
func parseRequest(r *http.Request) (job.StressRequest, error) {
	merged := map[string]any{}

	for key, vals := range r.URL.Query() {
		if len(vals) > 0 {
			merged[key] = vals[0]
		}
	}

	if r.Body != nil {
		raw, _ := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
		if len(bytes.TrimSpace(raw)) > 0 {
			var body map[string]any
			if err := json.Unmarshal(raw, &body); err != nil {
				return job.StressRequest{}, errInvalidBody
			}
			for key, v := range body {
				merged[key] = v
			}
		}
	}

	rawMerged, err := json.Marshal(merged)
	if err != nil {
		return job.StressRequest{}, err
	}

	var req job.StressRequest
	if err := json.Unmarshal(rawMerged, &req); err != nil {
		return job.StressRequest{}, err
	}

	req.Normalize()
	return req, nil
}
