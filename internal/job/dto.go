package job

import (
	"math"
	"sync/atomic"
	"time"
)

const mb = 1024 * 1024

// PublicMetrics is the externally exposed metrics shape (bytes → MB).
type PublicMetrics struct {
	DiskWrittenMb    float64 `json:"diskWrittenMb"`
	DiskReadMb       float64 `json:"diskReadMb"`
	NetworkWrittenMb float64 `json:"networkWrittenMb"`
	NetworkReadMb    float64 `json:"networkReadMb"`
}

// PublicJob is the API-facing representation of a job. It hides internal
// machinery (context, timer, goroutines) and converts bytes to MB.
// Mirrors publicJob() from the original implementation.
type PublicJob struct {
	ID           string        `json:"id"`
	Status       JobStatus     `json:"status"`
	CreatedAt    string        `json:"createdAt"`
	ScheduledFor string        `json:"scheduledFor"`
	StartedAt    *string       `json:"startedAt,omitempty"`
	FinishedAt   *string       `json:"finishedAt,omitempty"`
	Error        string        `json:"error,omitempty"`
	Request      StressRequest `json:"request"`
	Metrics      PublicMetrics `json:"metrics"`
}

// Public builds the API representation of a job, reading mutable fields
// under the store's read lock for consistency.
func (s *Store) Public(j *Job) PublicJob {
	s.mu.RLock()
	status := j.Status
	startedAt := isoPtr(j.StartedAt)
	finishedAt := isoPtr(j.FinishedAt)
	errMsg := j.Err
	s.mu.RUnlock()

	return PublicJob{
		ID:           j.ID,
		Status:       status,
		CreatedAt:    iso(j.CreatedAt),
		ScheduledFor: iso(j.ScheduledFor),
		StartedAt:    startedAt,
		FinishedAt:   finishedAt,
		Error:        errMsg,
		Request:      j.Request,
		Metrics: PublicMetrics{
			DiskWrittenMb:    bytesToMb(atomic.LoadInt64(&j.Metrics.DiskWrittenBytes)),
			DiskReadMb:       bytesToMb(atomic.LoadInt64(&j.Metrics.DiskReadBytes)),
			NetworkWrittenMb: bytesToMb(atomic.LoadInt64(&j.Metrics.NetworkWrittenBytes)),
			NetworkReadMb:    bytesToMb(atomic.LoadInt64(&j.Metrics.NetworkReadBytes)),
		},
	}
}

func bytesToMb(b int64) float64 {
	return math.Round((float64(b)/mb)*100) / 100
}

func iso(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

func isoPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := iso(*t)
	return &s
}
