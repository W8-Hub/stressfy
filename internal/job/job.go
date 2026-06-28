package job

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Number is a float64 that unmarshals from either a JSON number or a numeric
// string. This keeps the API as permissive as the original TypeScript service,
// where query params arrive as strings and bodies as numbers. An unparseable
// value is tolerated and left as zero.
type Number float64

func (n *Number) UnmarshalJSON(b []byte) error {
	s := string(b)
	if s == "null" {
		return nil
	}
	var f float64
	if err := json.Unmarshal(b, &f); err == nil {
		*n = Number(f)
		return nil
	}
	var str string
	if err := json.Unmarshal(b, &str); err == nil {
		str = strings.TrimSpace(str)
		if str == "" {
			return nil
		}
		if f, err := strconv.ParseFloat(str, 64); err == nil {
			*n = Number(f)
		}
	}
	return nil
}

// Val returns the float64 value of n, or (0, false) when n is nil.
func (n *Number) Val() (float64, bool) {
	if n == nil {
		return 0, false
	}
	return float64(*n), true
}

// JobStatus represents the lifecycle state of a job.
// scheduled → running → stopping → finished | failed | cancelled
type JobStatus string

const (
	StatusScheduled JobStatus = "scheduled"
	StatusRunning   JobStatus = "running"
	StatusStopping  JobStatus = "stopping"
	StatusFinished  JobStatus = "finished"
	StatusFailed    JobStatus = "failed"
	StatusCancelled JobStatus = "cancelled"
)

// DiskSpec configures a disk stress operation.
type DiskSpec struct {
	MB       *Number `json:"mb,omitempty"`
	MBps     *Number `json:"mbps,omitempty"`
	Path     *string `json:"path,omitempty"`
	KeepFile *bool   `json:"keepFile,omitempty"`
	Fsync    *bool   `json:"fsync,omitempty"`
}

// NetworkSpec configures a network stress operation.
type NetworkSpec struct {
	URL  string  `json:"url"`
	MB   *Number `json:"mb,omitempty"`
	MBps *Number `json:"mbps,omitempty"`
}

// StressRequest is the parsed stress job request. All scalar fields are
// pointers so we can distinguish "absent" from "zero" and apply aliases,
// mirroring the permissive shape of the original TypeScript API.
type StressRequest struct {
	StartAt *string `json:"startAt,omitempty"`
	Start   *string `json:"start,omitempty"`

	DurationSec *Number `json:"durationSec,omitempty"`
	Time        *Number `json:"time,omitempty"`

	CPUPercent *Number `json:"cpuPercent,omitempty"`
	CPU        *Number `json:"cpu,omitempty"`

	RAMPercent *Number `json:"ramPercent,omitempty"`
	RAM        *Number `json:"ram,omitempty"`
	RAMMb      *Number `json:"ramMb,omitempty"`

	DiskWrite *DiskSpec `json:"diskWrite,omitempty"`
	DiskRead  *DiskSpec `json:"diskRead,omitempty"`

	NetworkWrite *NetworkSpec `json:"networkWrite,omitempty"`
	NetworkRead  *NetworkSpec `json:"networkRead,omitempty"`
}

// Normalize fills the canonical (long) field names from their short aliases
// when the canonical field is absent. Mirrors normalizeRequest().
func (r *StressRequest) Normalize() {
	if r.StartAt == nil {
		r.StartAt = r.Start
	}
	if r.DurationSec == nil {
		r.DurationSec = r.Time
	}
	if r.CPUPercent == nil {
		r.CPUPercent = r.CPU
	}
	if r.RAMPercent == nil {
		r.RAMPercent = r.RAM
	}
}

// Metrics holds the running byte counters for a job (accessed atomically).
type Metrics struct {
	DiskWrittenBytes    int64
	DiskReadBytes       int64
	NetworkWrittenBytes int64
	NetworkReadBytes    int64
}

// Job is the in-memory unit of stress work.
type Job struct {
	ID           string
	CreatedAt    time.Time
	ScheduledFor time.Time
	Request      StressRequest
	Duration     time.Duration

	Ctx    context.Context
	Cancel context.CancelFunc
	Timer  *time.Timer

	Metrics Metrics

	// Mutable state guarded by the owning Store's mutex.
	Status     JobStatus
	StartedAt  *time.Time
	FinishedAt *time.Time
	Err        string

	mu    sync.Mutex
	files []string
}

// AddFile registers a file created during stress so it can be cleaned up.
func (j *Job) AddFile(path string) {
	j.mu.Lock()
	j.files = append(j.files, path)
	j.mu.Unlock()
}

// Files returns a snapshot of registered files.
func (j *Job) Files() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]string, len(j.files))
	copy(out, j.files)
	return out
}
