package mock

import (
	"testing"
	"time"
)

func TestDefaultCode(t *testing.T) {
	c := NewController()
	if c.Current() != 200 {
		t.Errorf("default = %d, want 200", c.Current())
	}
	if st := c.State(); st.StatusCode != 200 || st.ScheduledCode != nil {
		t.Errorf("unexpected initial state: %+v", st)
	}
}

func TestScheduleImmediate(t *testing.T) {
	c := NewController()
	c.Schedule(503, time.Now(), 0)
	if c.Current() != 503 {
		t.Errorf("code = %d, want 503", c.Current())
	}
	if st := c.State(); st.ScheduledCode != nil {
		t.Errorf("immediate swap should not leave a pending schedule: %+v", st)
	}
}

func TestSchedulePastIsImmediate(t *testing.T) {
	c := NewController()
	c.Schedule(500, time.Now().Add(-time.Minute), 0)
	if c.Current() != 500 {
		t.Errorf("code = %d, want 500", c.Current())
	}
}

func TestScheduleFutureNotAppliedYet(t *testing.T) {
	c := NewController()
	c.Schedule(503, time.Now().Add(time.Hour), 0)
	if c.Current() != 200 {
		t.Errorf("future swap should not change code yet, got %d", c.Current())
	}
	st := c.State()
	if st.ScheduledCode == nil || *st.ScheduledCode != 503 || st.ScheduledFor == nil {
		t.Errorf("pending schedule not reported: %+v", st)
	}
}

func TestScheduleFutureApplies(t *testing.T) {
	c := NewController()
	c.Schedule(503, time.Now().Add(40*time.Millisecond), 0)
	time.Sleep(120 * time.Millisecond)
	if c.Current() != 503 {
		t.Errorf("scheduled swap did not apply, got %d", c.Current())
	}
	if st := c.State(); st.ScheduledCode != nil {
		t.Errorf("schedule should be cleared after applying: %+v", st)
	}
}

func TestAutoRevert(t *testing.T) {
	c := NewController()
	c.Schedule(503, time.Now(), 40*time.Millisecond)
	if c.Current() != 503 {
		t.Fatalf("code = %d, want 503", c.Current())
	}
	time.Sleep(120 * time.Millisecond)
	if c.Current() != 200 {
		t.Errorf("auto-revert did not restore 200, got %d", c.Current())
	}
}

func TestScheduleCancelsPrevious(t *testing.T) {
	c := NewController()
	// First schedule a future swap, then replace it before it fires.
	c.Schedule(500, time.Now().Add(50*time.Millisecond), 0)
	c.Schedule(418, time.Now(), 0) // immediate, cancels the pending 500
	time.Sleep(120 * time.Millisecond)
	if c.Current() != 418 {
		t.Errorf("code = %d, want 418 (previous schedule should be cancelled)", c.Current())
	}
}

func TestReset(t *testing.T) {
	c := NewController()
	c.Schedule(503, time.Now(), time.Hour)
	c.Reset()
	if c.Current() != 200 {
		t.Errorf("reset code = %d, want 200", c.Current())
	}
	if st := c.State(); st.RevertAt != nil {
		t.Error("reset should clear pending revert")
	}
}
