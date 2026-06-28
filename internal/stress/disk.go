package stress

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"stressfy/internal/config"
	"stressfy/internal/job"
)

// RunDiskWrite writes a file continuously up to a target size (or until the
// duration elapses), with optional rate limiting and fsync.
func RunDiskWrite(ctx context.Context, cfg config.Config, j *job.Job, spec *job.DiskSpec) error {
	if spec == nil {
		return nil
	}

	dir := specPath(spec, cfg.DataDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	targetMb := config.ClampNumber(numOr(spec.MB, cfg.MaxDiskMB), 1, cfg.MaxDiskMB, 512)
	mbps := optMbps(spec.MBps)

	file := filepath.Join(dir, j.ID+"-write.dat")
	j.AddFile(file)

	f, err := os.Create(file)
	if err != nil {
		return err
	}

	chunk := bytes.Repeat([]byte{0x77}, MB)
	start := time.Now()
	end := start.Add(j.Duration)
	targetBytes := int64(targetMb * MB)
	var written int64

	defer func() {
		f.Close()
		if !boolVal(spec.KeepFile) {
			_ = os.Remove(file)
		}
	}()

	for ctx.Err() == nil && time.Now().Before(end) && written < targetBytes {
		size := int64(len(chunk))
		if remaining := targetBytes - written; remaining < size {
			size = remaining
		}

		n, err := f.Write(chunk[:size])
		if err != nil {
			return err
		}
		written += int64(n)
		atomic.AddInt64(&j.Metrics.DiskWrittenBytes, int64(n))

		if boolVal(spec.Fsync) {
			if err := f.Sync(); err != nil {
				return err
			}
		}

		throttle(ctx, written, mbps, start)
	}

	return nil
}

// RunDiskRead creates a seed file and re-reads it in a loop for the duration,
// with optional rate limiting.
func RunDiskRead(ctx context.Context, cfg config.Config, j *job.Job, spec *job.DiskSpec) error {
	if spec == nil {
		return nil
	}

	dir := specPath(spec, cfg.DataDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	targetMb := config.ClampNumber(numOr(spec.MB, 512), 1, cfg.MaxDiskMB, 512)
	mbps := optMbps(spec.MBps)

	file := filepath.Join(dir, j.ID+"-read-seed.dat")
	j.AddFile(file)

	if err := createSeedFile(file, int(targetMb)); err != nil {
		return err
	}

	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer func() {
		f.Close()
		_ = os.Remove(file)
	}()

	chunk := make([]byte, MB)
	start := time.Now()
	end := start.Add(j.Duration)
	var readTotal int64
	var position int64

	for ctx.Err() == nil && time.Now().Before(end) {
		n, err := f.ReadAt(chunk, position)
		if n > 0 {
			position += int64(n)
			readTotal += int64(n)
			atomic.AddInt64(&j.Metrics.DiskReadBytes, int64(n))
			throttle(ctx, readTotal, mbps, start)
		}
		if n == 0 || err != nil {
			// EOF or short read: rewind and keep going.
			position = 0
		}
	}

	return nil
}

func createSeedFile(path string, sizeMb int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	chunk := bytes.Repeat([]byte{0x61}, MB)
	for i := 0; i < sizeMb; i++ {
		if _, err := f.Write(chunk); err != nil {
			return err
		}
	}
	return nil
}

func specPath(spec *job.DiskSpec, fallback string) string {
	if spec.Path != nil && *spec.Path != "" {
		return *spec.Path
	}
	return fallback
}

func boolVal(b *bool) bool {
	return b != nil && *b
}

// numOr returns the Number's value, or fallback when absent.
func numOr(n *job.Number, fallback float64) float64 {
	if v, ok := n.Val(); ok {
		return v
	}
	return fallback
}

// optMbps mirrors `spec.mbps ? clamp(...) : 0`.
func optMbps(n *job.Number) float64 {
	if v, ok := n.Val(); ok && v != 0 {
		return config.ClampNumber(v, 1, 100000, 0)
	}
	return 0
}
