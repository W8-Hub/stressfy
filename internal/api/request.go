package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"stressfy/internal/job"
)

// maxBodyBytes caps how much of the request body we read when parsing job specs.
const maxBodyBytes = 1 << 20 // 1 MiB

// parseRequest builds a StressRequest by merging query parameters and the JSON
// body, with the body taking precedence — mirroring the original `{...query,
// ...body}` behavior. Query values arrive as strings and are tolerated by the
// Number type. A body that is not a valid JSON object is ignored (treated as
// empty), matching the permissive `typeof body === 'object' ? body : {}` guard.
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
			if err := json.Unmarshal(raw, &body); err == nil {
				for key, v := range body {
					merged[key] = v
				}
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
