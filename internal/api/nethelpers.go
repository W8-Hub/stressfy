package api

import (
	"bytes"
	"io"
	"net/http"
	"strconv"

	"stressfy/internal/config"
)

// netSource handles GET /net/source — streams N MB of data, used as the
// counterpart for networkRead stress jobs.
func (s *Server) netSource(w http.ResponseWriter, r *http.Request) {
	mbVal := config.ClampNumber(r.URL.Query().Get("mb"), 1, s.cfg.MaxNetMB, 100)
	chunkMb := config.ClampNumber(r.URL.Query().Get("chunkMb"), 1, 16, 1)

	totalBytes := int64(mbVal * config.MB)
	chunkBytes := int(chunkMb * config.MB)

	w.Header().Set("content-type", "application/octet-stream")
	w.Header().Set("content-length", strconv.FormatInt(totalBytes, 10))
	w.WriteHeader(http.StatusOK)

	chunk := bytes.Repeat([]byte{0x73}, chunkBytes)
	var sent int64
	for sent < totalBytes {
		size := int64(len(chunk))
		if remaining := totalBytes - sent; remaining < size {
			size = remaining
		}
		n, err := w.Write(chunk[:size])
		if err != nil {
			return
		}
		sent += int64(n)
	}
}

// netSink handles POST /net/sink — drains the request body and reports how many
// bytes were received, used as the counterpart for networkWrite stress jobs.
func (s *Server) netSink(w http.ResponseWriter, r *http.Request) {
	bytesRead, _ := io.Copy(io.Discard, r.Body)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"receivedBytes": bytesRead,
		"receivedMb":    round2(float64(bytesRead) / mb),
	})
}
