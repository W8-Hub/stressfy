package job

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Store holds all jobs in memory. State is not persisted; restarting the
// process clears everything. The mutex guards the map and the mutable
// status fields of every job it owns.
type Store struct {
	mu   sync.RWMutex
	jobs map[string]*Job
}

// NewStore creates an empty job store.
func NewStore() *Store {
	return &Store{jobs: make(map[string]*Job)}
}

// Create registers a new job and schedules run to fire at scheduledFor.
// The job starts in "scheduled" state if scheduledFor is in the future,
// otherwise "running".
func (s *Store) Create(req StressRequest, scheduledFor time.Time, duration time.Duration, run func(*Job)) *Job {
	ctx, cancel := context.WithCancel(context.Background())

	status := StatusRunning
	if scheduledFor.After(time.Now()) {
		status = StatusScheduled
	}

	j := &Job{
		ID:           uuid.NewString(),
		CreatedAt:    time.Now(),
		ScheduledFor: scheduledFor,
		Request:      req,
		Duration:     duration,
		Ctx:          ctx,
		Cancel:       cancel,
		Status:       status,
	}

	s.mu.Lock()
	s.jobs[j.ID] = j
	s.mu.Unlock()

	delay := time.Until(scheduledFor)
	if delay < 0 {
		delay = 0
	}
	j.Timer = time.AfterFunc(delay, func() { run(j) })

	return j
}

// Get returns a job by id.
func (s *Store) Get(id string) (*Job, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	return j, ok
}

// List returns all jobs.
func (s *Store) List() []*Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		out = append(out, j)
	}
	return out
}

// MarkRunning transitions a job into the running state and stamps StartedAt.
func (s *Store) MarkRunning(j *Job) {
	now := time.Now()
	s.mu.Lock()
	j.Status = StatusRunning
	j.StartedAt = &now
	s.mu.Unlock()
}

// Finish stamps FinishedAt and sets the final status and error.
func (s *Store) Finish(j *Job, status JobStatus, errMsg string) {
	now := time.Now()
	s.mu.Lock()
	j.Status = status
	j.FinishedAt = &now
	if errMsg != "" {
		j.Err = errMsg
	}
	s.mu.Unlock()
}

// Stop cancels a job. For scheduled jobs it transitions straight to
// cancelled; for running jobs it sets "stopping" and lets the runner
// finalize the status. Already-terminal jobs are left untouched.
// Returns false if the job does not exist.
func (s *Store) Stop(id string) (*Job, bool) {
	s.mu.Lock()
	j, ok := s.jobs[id]
	if !ok {
		s.mu.Unlock()
		return nil, false
	}

	if j.Status == StatusFinished || j.Status == StatusFailed || j.Status == StatusCancelled {
		s.mu.Unlock()
		return j, true
	}

	wasScheduled := j.Status == StatusScheduled
	if wasScheduled {
		now := time.Now()
		j.Status = StatusCancelled
		j.FinishedAt = &now
	} else {
		j.Status = StatusStopping
	}
	timer := j.Timer
	s.mu.Unlock()

	if timer != nil {
		timer.Stop()
	}
	j.Cancel()

	return j, true
}

// Status returns the current status under the read lock.
func (s *Store) Status(j *Job) JobStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return j.Status
}
