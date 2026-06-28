package stress

import (
	"context"
	"math"
	"math/rand"
	"runtime"
	"sync"
	"time"
)

// RunCPU saturates the CPU at the given target percent across all cores for the
// given duration, using one goroutine per CPU. Replaces the worker_threads
// implementation of the original service.
func RunCPU(ctx context.Context, percent float64, duration time.Duration) {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cpuWorker(ctx, percent, duration)
		}()
	}
	wg.Wait()
}

func cpuWorker(ctx context.Context, percent float64, duration time.Duration) {
	const window = 100 * time.Millisecond
	busyDur := time.Duration(float64(window) * percent / 100)
	idleDur := window - busyDur

	end := time.Now().Add(duration)

	for time.Now().Before(end) {
		if ctx.Err() != nil {
			return
		}
		if busyDur > 0 {
			busy(ctx, busyDur)
		}
		if idleDur > 0 {
			if !sleepCtx(ctx, idleDur) {
				return
			}
		}
	}
}

// busy burns CPU cycles for d, bailing out early on cancellation.
func busy(ctx context.Context, d time.Duration) {
	end := time.Now().Add(d)
	x := 0.0
	for i := 0; ; i++ {
		// Check time/cancellation periodically rather than every iteration to
		// keep the inner loop hot.
		if i&1023 == 0 {
			if ctx.Err() != nil || !time.Now().Before(end) {
				break
			}
		}
		x += math.Sqrt(rand.Float64() * 1e9)
	}
	_ = x
}
