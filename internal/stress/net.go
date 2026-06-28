package stress

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"stressfy/internal/config"
	"stressfy/internal/job"
)

// httpClient has no timeout; cancellation is driven by the job context.
var httpClient = &http.Client{}

// RunNetworkWrite uploads data to a URL in streaming POST requests, repeating
// for the job duration.
func RunNetworkWrite(ctx context.Context, cfg config.Config, j *job.Job, spec *job.NetworkSpec) error {
	if spec == nil || spec.URL == "" {
		return nil
	}

	mb := config.ClampNumber(numOr(spec.MB, cfg.MaxNetMB), 1, cfg.MaxNetMB, 512)
	mbps := optMbps(spec.MBps)
	end := time.Now().Add(j.Duration)

	for ctx.Err() == nil && time.Now().Before(end) {
		if err := uploadOnce(ctx, spec.URL, mb, mbps, j); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
	return nil
}

// RunNetworkRead downloads data from a URL in streaming GET requests, repeating
// for the job duration.
func RunNetworkRead(ctx context.Context, j *job.Job, spec *job.NetworkSpec) error {
	if spec == nil || spec.URL == "" {
		return nil
	}

	mbps := optMbps(spec.MBps)
	end := time.Now().Add(j.Duration)

	for ctx.Err() == nil && time.Now().Before(end) {
		if err := downloadOnce(ctx, spec.URL, mbps, j); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
	return nil
}

func uploadOnce(ctx context.Context, url string, totalMb, mbps float64, j *job.Job) error {
	pr, pw := io.Pipe()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, pr)
	if err != nil {
		pr.Close()
		return err
	}
	req.Header.Set("content-type", "application/octet-stream")

	go func() {
		chunk := bytes.Repeat([]byte{0x6e}, MB)
		totalBytes := int64(totalMb * MB)
		start := time.Now()
		var sent int64

		for sent < totalBytes {
			if ctx.Err() != nil {
				pw.CloseWithError(ctx.Err())
				return
			}
			size := int64(len(chunk))
			if remaining := totalBytes - sent; remaining < size {
				size = remaining
			}
			n, err := pw.Write(chunk[:size])
			if err != nil {
				return
			}
			sent += int64(n)
			atomic.AddInt64(&j.Metrics.NetworkWrittenBytes, int64(n))
			throttle(ctx, sent, mbps, start)
		}
		pw.Close()
	}()

	resp, err := httpClient.Do(req)
	if err != nil {
		pr.CloseWithError(err)
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func downloadOnce(ctx context.Context, url string, mbps float64, j *job.Job) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	buf := make([]byte, MB)
	start := time.Now()
	var received int64

	for {
		if ctx.Err() != nil {
			return nil
		}
		n, err := resp.Body.Read(buf)
		if n > 0 {
			received += int64(n)
			atomic.AddInt64(&j.Metrics.NetworkReadBytes, int64(n))
			throttle(ctx, received, mbps, start)
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
