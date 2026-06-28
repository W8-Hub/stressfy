package config

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const MB = 1024 * 1024

// Config holds the runtime configuration loaded from environment variables.
// Mirrors the env handling of the original TypeScript implementation.
type Config struct {
	Port           int
	DataDir        string
	TZOffset       string
	MaxDurationSec float64
	MaxRAMPercent  float64
	MaxDiskMB      float64
	MaxNetMB       float64
	MaxLatencyMS   float64
}

// Load reads configuration from the environment, applying the same defaults
// as the original service.
func Load() Config {
	return Config{
		Port:           int(envNumber("PORT", 3333)),
		DataDir:        envString("DATA_DIR", "/tmp/stress-api"),
		TZOffset:       envString("TZ_OFFSET", "-03:00"),
		MaxDurationSec: envNumber("MAX_DURATION_SEC", 900),
		MaxRAMPercent:  envNumber("MAX_RAM_PERCENT", 85),
		MaxDiskMB:      envNumber("MAX_DISK_MB", 10240),
		MaxNetMB:       envNumber("MAX_NET_MB", 10240),
		MaxLatencyMS:   envNumber("MAX_LATENCY_MS", 60000),
	}
}

func envString(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func envNumber(key string, fallback float64) float64 {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return n
		}
	}
	return fallback
}

// ClampNumber coerces an arbitrary value (number or numeric string) into a
// float64 bounded by [min, max], falling back to fallback when not finite.
// Mirrors clampNumber() from the original implementation.
func ClampNumber(value any, min, max, fallback float64) float64 {
	n, ok := toFloat(value)
	if !ok {
		n = fallback
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

func toFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case nil:
		return 0, false
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case bool:
		return 0, false
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

var (
	legacyStartRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})[:\s](\d{2}:\d{2}:\d{2})$`)
	naiveStartRe  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}$`)
)

// ParseStartAt parses a start timestamp, applying the configured timezone
// offset to formats that lack one. Mirrors parseStartAt() from the original.
// Returns the current time when input is empty.
func (c Config) ParseStartAt(input string) (time.Time, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return time.Now(), nil
	}

	if m := legacyStartRe.FindStringSubmatch(value); m != nil {
		value = fmt.Sprintf("%sT%s%s", m[1], m[2], c.TZOffset)
	} else if naiveStartRe.MatchString(value) {
		value = value + c.TZOffset
	}

	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid startAt/start format: %s", input)
	}
	return t, nil
}
