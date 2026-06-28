// Package mock provides controllable "chaos" endpoints — a mockable status
// code, random 5xx errors and intentional latency — for exercising proxies,
// health checks, monitors and retry logic.
package mock

import (
	"sync"
	"time"
)

// DefaultCode is the status code returned before any swap is scheduled.
const DefaultCode = 200

// State is the externally visible snapshot of the controller.
type State struct {
	StatusCode    int        `json:"statusCode"`
	ScheduledCode *int       `json:"scheduledCode,omitempty"`
	ScheduledFor  *time.Time `json:"scheduledFor,omitempty"`
	RevertAt      *time.Time `json:"revertAt,omitempty"`
}

// Controller holds the current mocked status code and any pending scheduled
// swap / auto-revert. It is safe for concurrent use.
type Controller struct {
	mu          sync.Mutex
	code        int
	swapTimer   *time.Timer
	revertTimer *time.Timer

	// pending describes a not-yet-applied scheduled swap (nil once applied).
	scheduledCode *int
	scheduledFor  *time.Time
	revertAt      *time.Time
}

// NewController returns a controller serving DefaultCode.
func NewController() *Controller {
	return &Controller{code: DefaultCode}
}

// Current returns the active status code.
func (c *Controller) Current() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.code
}

// State returns a snapshot of the current code and any pending swap.
func (c *Controller) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return State{
		StatusCode:    c.code,
		ScheduledCode: c.scheduledCode,
		ScheduledFor:  c.scheduledFor,
		RevertAt:      c.revertAt,
	}
}

// Schedule sets the status code to code at startAt (immediately if startAt is
// now or in the past). When revertAfter > 0, the code reverts to DefaultCode
// that long after it is applied. Any previously pending swap/revert is cancelled.
func (c *Controller) Schedule(code int, startAt time.Time, revertAfter time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cancelTimersLocked()
	c.scheduledCode = nil
	c.scheduledFor = nil
	c.revertAt = nil

	delay := time.Until(startAt)
	if delay <= 0 {
		c.applyLocked(code, revertAfter)
		return
	}

	// Pending future swap.
	codeCopy := code
	when := startAt
	c.scheduledCode = &codeCopy
	c.scheduledFor = &when
	if revertAfter > 0 {
		r := startAt.Add(revertAfter)
		c.revertAt = &r
	}

	c.swapTimer = time.AfterFunc(delay, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.scheduledCode = nil
		c.scheduledFor = nil
		c.applyLocked(code, revertAfter)
	})
}

// Reset cancels pending timers and restores DefaultCode.
func (c *Controller) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancelTimersLocked()
	c.scheduledCode = nil
	c.scheduledFor = nil
	c.revertAt = nil
	c.code = DefaultCode
}

// applyLocked sets the code now and arms the auto-revert if requested.
// Caller must hold c.mu.
func (c *Controller) applyLocked(code int, revertAfter time.Duration) {
	c.code = code

	if revertAfter <= 0 {
		c.revertAt = nil
		return
	}

	when := time.Now().Add(revertAfter)
	c.revertAt = &when
	c.revertTimer = time.AfterFunc(revertAfter, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.code = DefaultCode
		c.revertAt = nil
	})
}

// cancelTimersLocked stops any pending timers. Caller must hold c.mu.
func (c *Controller) cancelTimersLocked() {
	if c.swapTimer != nil {
		c.swapTimer.Stop()
		c.swapTimer = nil
	}
	if c.revertTimer != nil {
		c.revertTimer.Stop()
		c.revertTimer = nil
	}
}
