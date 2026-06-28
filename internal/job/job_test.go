package job

import (
	"encoding/json"
	"testing"
)

func TestNumberUnmarshal(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantVal float64
		wantNil bool
	}{
		{"number", `{"cpu":50}`, 50, false},
		{"numeric string", `{"cpu":"75"}`, 75, false},
		{"null leaves nil", `{"cpu":null}`, 0, true},
		{"absent leaves nil", `{}`, 0, true},
		{"empty string leaves zero", `{"cpu":""}`, 0, false},
		{"invalid string tolerated as zero", `{"cpu":"abc"}`, 0, false},
		{"float", `{"cpu":12.5}`, 12.5, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var r StressRequest
			if err := json.Unmarshal([]byte(c.input), &r); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}
			if c.wantNil {
				if r.CPU != nil {
					t.Fatalf("expected nil, got %v", *r.CPU)
				}
				return
			}
			if r.CPU == nil {
				t.Fatal("expected non-nil CPU")
			}
			got, ok := r.CPU.Val()
			if !ok || got != c.wantVal {
				t.Fatalf("Val() = (%v, %v), want (%v, true)", got, ok, c.wantVal)
			}
		})
	}
}

func TestNumberValNil(t *testing.T) {
	var n *Number
	if v, ok := n.Val(); ok || v != 0 {
		t.Fatalf("nil Number Val() = (%v, %v), want (0, false)", v, ok)
	}
}

func TestNormalizeAliases(t *testing.T) {
	mk := func(f float64) *Number { n := Number(f); return &n }
	s := func(v string) *string { return &v }

	r := StressRequest{
		Start: s("2030-01-01T00:00:00"),
		Time:  mk(60),
		CPU:   mk(80),
		RAM:   mk(50),
	}
	r.Normalize()

	if r.StartAt == nil || *r.StartAt != "2030-01-01T00:00:00" {
		t.Error("StartAt not filled from Start")
	}
	if v, _ := r.DurationSec.Val(); v != 60 {
		t.Error("DurationSec not filled from Time")
	}
	if v, _ := r.CPUPercent.Val(); v != 80 {
		t.Error("CPUPercent not filled from CPU")
	}
	if v, _ := r.RAMPercent.Val(); v != 50 {
		t.Error("RAMPercent not filled from RAM")
	}
}

func TestNormalizeCanonicalWins(t *testing.T) {
	mk := func(f float64) *Number { n := Number(f); return &n }
	r := StressRequest{
		DurationSec: mk(120),
		Time:        mk(60), // alias should not override the canonical field
	}
	r.Normalize()
	if v, _ := r.DurationSec.Val(); v != 120 {
		t.Errorf("DurationSec = %v, want 120 (canonical should win)", v)
	}
}
