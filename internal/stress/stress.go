package stress

import (
	"context"
	"os"
	"sync"
	"time"

	"stressfy/internal/config"
	"stressfy/internal/job"
)

const MB = 1024 * 1024

// RunJob orchestrates all requested stress operations for a job, then finalizes
// its status and cleans up. Each operation self-limits to the job duration; the
// job's context is cancelled only on an explicit stop.
func RunJob(store *job.Store, cfg config.Config, j *job.Job) {
	store.MarkRunning(j)

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		failErr error
	)

	record := func(err error) {
		if err == nil || j.Ctx.Err() != nil {
			return
		}
		mu.Lock()
		if failErr == nil {
			failErr = err
		}
		mu.Unlock()
	}

	spawn := func(fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			record(fn())
		}()
	}

	// CPU runs for the job duration alongside the other stressors.
	if cpu, ok := j.Request.CPUPercent.Val(); ok && cpu > 0 {
		spawn(func() error {
			RunCPU(j.Ctx, cpu, j.Duration)
			return nil
		})
	}

	spawn(func() error { return RunRAM(j.Ctx, cfg, j) })
	spawn(func() error { return RunDiskWrite(j.Ctx, cfg, j, j.Request.DiskWrite) })
	spawn(func() error { return RunDiskRead(j.Ctx, cfg, j, j.Request.DiskRead) })
	spawn(func() error { return RunNetworkWrite(j.Ctx, cfg, j, j.Request.NetworkWrite) })
	spawn(func() error { return RunNetworkRead(j.Ctx, j, j.Request.NetworkRead) })

	wg.Wait()

	status := job.StatusFinished
	errMsg := ""
	switch {
	case j.Ctx.Err() != nil:
		status = job.StatusCancelled
	case failErr != nil:
		status = job.StatusFailed
		errMsg = failErr.Error()
	}
	store.Finish(j, status, errMsg)

	for _, f := range j.Files() {
		_ = os.Remove(f)
	}
}

// sleepCtx sleeps for d or until the context is cancelled. Returns false if it
// was interrupted by cancellation.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// throttle pauses so that transferredBytes since start does not exceed the
// configured rate (mbps). A non-positive mbps disables throttling.
func throttle(ctx context.Context, transferredBytes int64, mbps float64, start time.Time) {
	if mbps <= 0 {
		return
	}
	expected := time.Duration(float64(transferredBytes) / (mbps * MB) * float64(time.Second))
	actual := time.Since(start)
	if expected > actual {
		sleepCtx(ctx, expected-actual)
	}
}
