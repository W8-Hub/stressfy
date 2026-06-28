package config

import (
	"testing"
)

func TestClampNumber(t *testing.T) {
	cases := []struct {
		name                     string
		value                    any
		min, max, fallback, want float64
	}{
		{"number in range", 50.0, 0, 100, 10, 50},
		{"number below min", -5.0, 0, 100, 10, 0},
		{"number above max", 999.0, 0, 100, 10, 100},
		{"numeric string", "75", 0, 100, 10, 75},
		{"numeric string clamped", "150", 0, 100, 10, 100},
		{"nil uses fallback", nil, 0, 100, 10, 10},
		{"non-numeric string uses fallback", "abc", 0, 100, 10, 10},
		{"bool uses fallback", true, 0, 100, 10, 10},
		{"int value", 42, 0, 100, 10, 42},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ClampNumber(c.value, c.min, c.max, c.fallback); got != c.want {
				t.Fatalf("ClampNumber(%v) = %v, want %v", c.value, got, c.want)
			}
		})
	}
}

func TestParseStartAt(t *testing.T) {
	cfg := Config{TZOffset: "-03:00"}

	t.Run("empty returns now-ish without error", func(t *testing.T) {
		if _, err := cfg.ParseStartAt(""); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("naive ISO applies offset", func(t *testing.T) {
		got, err := cfg.ParseStartAt("2030-01-01T00:00:00")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := "2030-01-01T03:00:00Z"; got.UTC().Format("2006-01-02T15:04:05Z") != want {
			t.Fatalf("got %s, want %s", got.UTC().Format("2006-01-02T15:04:05Z"), want)
		}
	})

	t.Run("legacy format applies offset", func(t *testing.T) {
		got, err := cfg.ParseStartAt("2030-01-01:00:00:00")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := "2030-01-01T03:00:00Z"; got.UTC().Format("2006-01-02T15:04:05Z") != want {
			t.Fatalf("got %s, want %s", got.UTC().Format("2006-01-02T15:04:05Z"), want)
		}
	})

	t.Run("explicit offset is respected", func(t *testing.T) {
		got, err := cfg.ParseStartAt("2030-01-01T00:00:00+00:00")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := "2030-01-01T00:00:00Z"; got.UTC().Format("2006-01-02T15:04:05Z") != want {
			t.Fatalf("got %s, want %s", got.UTC().Format("2006-01-02T15:04:05Z"), want)
		}
	})

	t.Run("invalid returns error", func(t *testing.T) {
		if _, err := cfg.ParseStartAt("not-a-date"); err == nil {
			t.Fatal("expected error for invalid input")
		}
	})
}

func TestLoadDefaults(t *testing.T) {
	cfg := Load()
	if cfg.Port != 3333 {
		t.Errorf("Port = %d, want 3333", cfg.Port)
	}
	if cfg.MaxDurationSec != 900 {
		t.Errorf("MaxDurationSec = %v, want 900", cfg.MaxDurationSec)
	}
	if cfg.MaxRAMPercent != 85 {
		t.Errorf("MaxRAMPercent = %v, want 85", cfg.MaxRAMPercent)
	}
	if cfg.TZOffset != "-03:00" {
		t.Errorf("TZOffset = %q, want -03:00", cfg.TZOffset)
	}
	if cfg.MaxLatencyMS != 60000 {
		t.Errorf("MaxLatencyMS = %v, want 60000", cfg.MaxLatencyMS)
	}
}
