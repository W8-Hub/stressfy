package stress

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"stressfy/internal/config"
	"stressfy/internal/job"
)

func numPtr(f float64) *job.Number { n := job.Number(f); return &n }

func newJob(d time.Duration) *job.Job {
	ctx, cancel := context.WithCancel(context.Background())
	return &job.Job{ID: "test-job", Duration: d, Ctx: ctx, Cancel: cancel}
}

func testCfg(dir string) config.Config {
	return config.Config{DataDir: dir, MaxDiskMB: 10240, MaxNetMB: 10240, MaxRAMPercent: 85}
}

func TestSleepCtxCompletes(t *testing.T) {
	ctx := context.Background()
	if !sleepCtx(ctx, 10*time.Millisecond) {
		t.Error("sleepCtx should return true when it completes")
	}
}

func TestSleepCtxCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sleepCtx(ctx, time.Hour) {
		t.Error("sleepCtx should return false when cancelled")
	}
}

func TestThrottleSleeps(t *testing.T) {
	// 1 MB transferred at 10 MB/s => expected ~100ms.
	start := time.Now()
	throttle(context.Background(), MB, 10, start)
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Errorf("throttle returned too fast: %v", elapsed)
	}
}

func TestThrottleDisabled(t *testing.T) {
	start := time.Now()
	throttle(context.Background(), 100*MB, 0, start) // mbps=0 disables
	if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
		t.Errorf("throttle with mbps=0 should not sleep, took %v", elapsed)
	}
}

func TestRunCPURespectsDuration(t *testing.T) {
	j := newJob(80 * time.Millisecond)
	start := time.Now()
	RunCPU(j.Ctx, 50, j.Duration)
	elapsed := time.Since(start)
	if elapsed < 60*time.Millisecond {
		t.Errorf("RunCPU returned too early: %v", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Errorf("RunCPU ran too long: %v", elapsed)
	}
}

func TestRunCPUCancelled(t *testing.T) {
	j := newJob(10 * time.Second)
	go func() {
		time.Sleep(50 * time.Millisecond)
		j.Cancel()
	}()
	start := time.Now()
	RunCPU(j.Ctx, 80, j.Duration)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("RunCPU did not stop on cancel: %v", elapsed)
	}
}

func TestRunDiskWriteAndCleanup(t *testing.T) {
	dir := t.TempDir()
	cfg := testCfg(dir)
	j := newJob(2 * time.Second)
	spec := &job.DiskSpec{MB: numPtr(2)}

	if err := RunDiskWrite(j.Ctx, cfg, j, spec); err != nil {
		t.Fatalf("RunDiskWrite error: %v", err)
	}
	if got := atomic.LoadInt64(&j.Metrics.DiskWrittenBytes); got < 2*MB {
		t.Errorf("DiskWrittenBytes = %d, want >= %d", got, 2*MB)
	}
	// File should be removed (KeepFile not set).
	if _, err := os.Stat(filepath.Join(dir, j.ID+"-write.dat")); !os.IsNotExist(err) {
		t.Error("write file should have been removed")
	}
}

func TestRunDiskWriteKeepFile(t *testing.T) {
	dir := t.TempDir()
	cfg := testCfg(dir)
	j := newJob(2 * time.Second)
	keep := true
	spec := &job.DiskSpec{MB: numPtr(1), KeepFile: &keep}

	if err := RunDiskWrite(j.Ctx, cfg, j, spec); err != nil {
		t.Fatalf("error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, j.ID+"-write.dat")); err != nil {
		t.Errorf("keepFile=true should retain file: %v", err)
	}
}

func TestRunDiskReadAndCleanup(t *testing.T) {
	dir := t.TempDir()
	cfg := testCfg(dir)
	j := newJob(100 * time.Millisecond)
	spec := &job.DiskSpec{MB: numPtr(1)}

	if err := RunDiskRead(j.Ctx, cfg, j, spec); err != nil {
		t.Fatalf("RunDiskRead error: %v", err)
	}
	if got := atomic.LoadInt64(&j.Metrics.DiskReadBytes); got <= 0 {
		t.Error("DiskReadBytes should be > 0")
	}
	if _, err := os.Stat(filepath.Join(dir, j.ID+"-read-seed.dat")); !os.IsNotExist(err) {
		t.Error("seed file should have been removed")
	}
}

func TestRunDiskNilSpec(t *testing.T) {
	j := newJob(time.Second)
	if err := RunDiskWrite(j.Ctx, testCfg(t.TempDir()), j, nil); err != nil {
		t.Errorf("nil spec should be a noop, got %v", err)
	}
}

func TestRunRAMByMb(t *testing.T) {
	cfg := testCfg(t.TempDir())
	j := newJob(30 * time.Millisecond)
	j.Request = job.StressRequest{RAMMb: numPtr(8)}
	if err := RunRAM(j.Ctx, cfg, j); err != nil {
		t.Errorf("RunRAM error: %v", err)
	}
}

func TestRunRAMNoSpec(t *testing.T) {
	cfg := testCfg(t.TempDir())
	j := newJob(time.Second)
	if err := RunRAM(j.Ctx, cfg, j); err != nil {
		t.Errorf("RunRAM with no spec should noop, got %v", err)
	}
}

func TestRunNetworkWriteAndRead(t *testing.T) {
	var sunk int64
	mux := http.NewServeMux()
	mux.HandleFunc("/sink", func(w http.ResponseWriter, r *http.Request) {
		n, _ := io.Copy(io.Discard, r.Body)
		atomic.AddInt64(&sunk, n)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/source", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, 512*1024)) // 512 KB
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := testCfg(t.TempDir())

	jw := newJob(120 * time.Millisecond)
	if err := RunNetworkWrite(jw.Ctx, cfg, jw, &job.NetworkSpec{URL: srv.URL + "/sink", MB: numPtr(1)}); err != nil {
		t.Fatalf("RunNetworkWrite error: %v", err)
	}
	if atomic.LoadInt64(&jw.Metrics.NetworkWrittenBytes) <= 0 {
		t.Error("NetworkWrittenBytes should be > 0")
	}

	jr := newJob(120 * time.Millisecond)
	if err := RunNetworkRead(jr.Ctx, jr, &job.NetworkSpec{URL: srv.URL + "/source"}); err != nil {
		t.Fatalf("RunNetworkRead error: %v", err)
	}
	if atomic.LoadInt64(&jr.Metrics.NetworkReadBytes) <= 0 {
		t.Error("NetworkReadBytes should be > 0")
	}
}

func TestRunNetworkNilSpec(t *testing.T) {
	j := newJob(time.Second)
	if err := RunNetworkWrite(j.Ctx, testCfg(t.TempDir()), j, nil); err != nil {
		t.Errorf("nil spec noop, got %v", err)
	}
	if err := RunNetworkRead(j.Ctx, j, &job.NetworkSpec{}); err != nil {
		t.Errorf("empty url noop, got %v", err)
	}
}

func TestRunJobFinishes(t *testing.T) {
	store := job.NewStore()
	cfg := testCfg(t.TempDir())
	j := store.Create(
		job.StressRequest{CPUPercent: numPtr(20), DurationSec: numPtr(0.1)},
		time.Now().Add(time.Hour), // far future so the store timer doesn't fire it
		80*time.Millisecond,
		func(*job.Job) {},
	)

	RunJob(store, cfg, j)

	if store.Status(j) != job.StatusFinished {
		t.Errorf("status = %q, want finished", store.Status(j))
	}
	if j.FinishedAt == nil {
		t.Error("FinishedAt should be set")
	}
}

func TestRunJobCancelled(t *testing.T) {
	store := job.NewStore()
	cfg := testCfg(t.TempDir())
	j := store.Create(
		job.StressRequest{CPUPercent: numPtr(50), DurationSec: numPtr(10)},
		time.Now().Add(time.Hour),
		10*time.Second,
		func(*job.Job) {},
	)

	go func() {
		time.Sleep(50 * time.Millisecond)
		j.Cancel()
	}()

	RunJob(store, cfg, j)

	if store.Status(j) != job.StatusCancelled {
		t.Errorf("status = %q, want cancelled", store.Status(j))
	}
}
