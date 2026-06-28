package job

import (
	"sync/atomic"
	"testing"
	"time"
)

func noop(*Job) {}

func TestCreateImmediateIsRunning(t *testing.T) {
	s := NewStore()
	j := s.Create(StressRequest{}, time.Now().Add(-time.Second), time.Second, noop)
	if j.Status != StatusRunning {
		t.Errorf("status = %q, want running", j.Status)
	}
	if got, ok := s.Get(j.ID); !ok || got != j {
		t.Error("job not retrievable from store")
	}
}

func TestCreateFutureIsScheduled(t *testing.T) {
	s := NewStore()
	j := s.Create(StressRequest{}, time.Now().Add(time.Hour), time.Second, noop)
	if j.Status != StatusScheduled {
		t.Errorf("status = %q, want scheduled", j.Status)
	}
}

func TestStopScheduledCancels(t *testing.T) {
	s := NewStore()
	j := s.Create(StressRequest{}, time.Now().Add(time.Hour), time.Second, noop)

	got, ok := s.Stop(j.ID)
	if !ok {
		t.Fatal("Stop returned not found")
	}
	if got.Status != StatusCancelled {
		t.Errorf("status = %q, want cancelled", got.Status)
	}
	if j.Ctx.Err() == nil {
		t.Error("context should be cancelled after Stop")
	}
}

func TestStopRunningGoesToStopping(t *testing.T) {
	s := NewStore()
	j := s.Create(StressRequest{}, time.Now().Add(-time.Second), time.Second, noop)

	got, _ := s.Stop(j.ID)
	if got.Status != StatusStopping {
		t.Errorf("status = %q, want stopping", got.Status)
	}
}

func TestStopUnknown(t *testing.T) {
	s := NewStore()
	if _, ok := s.Stop("missing"); ok {
		t.Error("expected not found for unknown id")
	}
}

func TestStopTerminalIsNoop(t *testing.T) {
	s := NewStore()
	j := s.Create(StressRequest{}, time.Now().Add(-time.Second), time.Second, noop)
	s.Finish(j, StatusFinished, "")

	got, _ := s.Stop(j.ID)
	if got.Status != StatusFinished {
		t.Errorf("status = %q, want finished (stop on terminal is a noop)", got.Status)
	}
}

func TestMarkRunningAndFinish(t *testing.T) {
	s := NewStore()
	j := s.Create(StressRequest{}, time.Now().Add(time.Hour), time.Second, noop)

	s.MarkRunning(j)
	if j.Status != StatusRunning || j.StartedAt == nil {
		t.Error("MarkRunning did not set running/startedAt")
	}

	s.Finish(j, StatusFailed, "boom")
	if j.Status != StatusFailed || j.FinishedAt == nil || j.Err != "boom" {
		t.Error("Finish did not set failed/finishedAt/err")
	}
}

func TestPublicConvertsBytesToMb(t *testing.T) {
	s := NewStore()
	j := s.Create(StressRequest{}, time.Now().Add(time.Hour), time.Second, noop)

	atomic.StoreInt64(&j.Metrics.DiskWrittenBytes, 3*1024*1024)      // 3 MB
	atomic.StoreInt64(&j.Metrics.NetworkReadBytes, 1024*1024+524288) // 1.5 MB

	pub := s.Public(j)
	if pub.Metrics.DiskWrittenMb != 3 {
		t.Errorf("DiskWrittenMb = %v, want 3", pub.Metrics.DiskWrittenMb)
	}
	if pub.Metrics.NetworkReadMb != 1.5 {
		t.Errorf("NetworkReadMb = %v, want 1.5", pub.Metrics.NetworkReadMb)
	}
	if pub.ID != j.ID {
		t.Error("public id mismatch")
	}
}

func TestAddFiles(t *testing.T) {
	j := &Job{}
	j.AddFile("/tmp/a")
	j.AddFile("/tmp/b")
	files := j.Files()
	if len(files) != 2 || files[0] != "/tmp/a" || files[1] != "/tmp/b" {
		t.Errorf("Files() = %v", files)
	}
}
