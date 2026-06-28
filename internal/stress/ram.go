package stress

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"stressfy/internal/config"
	"stressfy/internal/job"
)

// RunRAM allocates resident memory up to a target (by ramMb or ramPercent),
// touching every page to force real allocation, holds it for the job duration,
// then releases it.
func RunRAM(ctx context.Context, cfg config.Config, j *job.Job) error {
	ramMb, hasMb := j.Request.RAMMb.Val()
	ramPercent, hasPct := j.Request.RAMPercent.Val()

	if !hasMb && !hasPct {
		return nil
	}

	limitBytes := memoryLimitBytes()

	var targetBytes float64
	if hasMb {
		targetBytes = ramMb * MB
	} else {
		percent := config.ClampNumber(ramPercent, 1, cfg.MaxRAMPercent, 0)
		targetBytes = limitBytes * percent / 100
	}

	toAllocate := targetBytes - float64(currentRSS())
	if toAllocate < 0 {
		toAllocate = 0
	}

	const chunkSize = 16 * MB
	var buffers [][]byte
	var allocated float64

	for allocated < toAllocate {
		if ctx.Err() != nil {
			return nil
		}

		size := int(chunkSize)
		if remaining := toAllocate - allocated; remaining < chunkSize {
			size = int(remaining)
		}
		if size <= 0 {
			break
		}

		buf := make([]byte, size)
		// Touch each page to force real (resident) allocation.
		for i := 0; i < len(buf); i += 4096 {
			buf[i] = 1
		}

		buffers = append(buffers, buf)
		allocated += float64(size)

		sleepCtx(ctx, 10*time.Millisecond)
	}

	// Hold the memory for the job duration, then release.
	sleepCtx(ctx, j.Duration)

	runtime.KeepAlive(buffers)
	return nil
}

// memoryLimitBytes returns the effective memory limit, preferring cgroup limits
// (v2 then v1) and falling back to total system memory.
func memoryLimitBytes() float64 {
	if v, ok := readNumberFile("/sys/fs/cgroup/memory.max"); ok && v > 0 {
		return v
	}
	if v, ok := readNumberFile("/sys/fs/cgroup/memory/memory.limit_in_bytes"); ok && v > 0 && v < 1<<62 {
		return v
	}
	return totalMemoryBytes()
}

// readNumberFile reads a single numeric value from a file. "max" yields false.
func readNumberFile(path string) (float64, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	s := strings.TrimSpace(string(raw))
	if s == "max" {
		return 0, false
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// totalMemoryBytes reads MemTotal from /proc/meminfo (Linux). On platforms
// without it, falls back to a conservative default so percent-based requests
// still allocate something.
func totalMemoryBytes() float64 {
	raw, err := os.ReadFile("/proc/meminfo")
	if err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(line, "MemTotal:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					if kb, err := strconv.ParseFloat(fields[1], 64); err == nil {
						return kb * 1024
					}
				}
			}
		}
	}
	const fallback = 8 * 1024 * MB // 8 GiB
	return fallback
}

// currentRSS returns the resident set size in bytes (Linux via /proc/self/statm).
// Returns 0 when unavailable, so the full target is allocated.
func currentRSS() int64 {
	raw, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(raw))
	if len(fields) < 2 {
		return 0
	}
	residentPages, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return 0
	}
	return residentPages * int64(os.Getpagesize())
}
