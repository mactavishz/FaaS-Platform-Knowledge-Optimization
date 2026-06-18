package autoscaler

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const DEFAULT_IDLE_DURATION_MINUTES = 15
const DEFAULT_CHECK_INTERVAL_SECONDS = 5
const DEFAULT_MAX_CONCURRENT_SCALE_DOWNS = 4

// Config holds the autoscaler configuration
type Config struct {
	Platform            string
	Enabled             bool
	DefaultIdleDuration time.Duration
	CheckInterval       time.Duration

	// MaxConcurrentScaleDowns bounds the worker pool used by the monitor's
	// idle scan. Each idle function's scale-down is dispatched to a worker
	// from this pool. Values <= 0 are coerced to DEFAULT_MAX_CONCURRENT_SCALE_DOWNS.
	// Set to 1 for sequential behavior; set higher for deployments with
	// many functions that can become idle simultaneously.
	MaxConcurrentScaleDowns int
}

// NewConfigFromEnv loads autoscaler configuration from environment variables.
func NewConfigFromEnv(platform string) (Config, error) {
	platform = strings.ToLower(platform)

	switch platform {
	case "faasd", "tinyfaas":
	default:
		return Config{}, fmt.Errorf("invalid platform: %s", platform)
	}

	enabled := os.Getenv("AUTOSCALER_ENABLED") == "true" || os.Getenv("AUTOSCALER_ENABLED") == "1"

	defaultDuration := DEFAULT_IDLE_DURATION_MINUTES * time.Minute // Default: 15 minutes
	if durationStr := os.Getenv("DEFAULT_SCALE_TO_ZERO_IDLE_DURATION"); durationStr != "" {
		if parsed, err := time.ParseDuration(durationStr); err == nil {
			defaultDuration = parsed
		} else {
			return Config{}, fmt.Errorf("invalid duration: %s", durationStr)
		}
	}

	// CheckInterval is how often the monitor scans for idle functions. It bounds
	// the scale-down detection latency: an idle function is scaled down at the
	// first scan tick after it crosses DefaultIdleDuration, so keep this well
	// below the idle duration. Configurable via AUTOSCALER_CHECK_INTERVAL.
	checkInterval := DEFAULT_CHECK_INTERVAL_SECONDS * time.Second
	if intervalStr := os.Getenv("AUTOSCALER_CHECK_INTERVAL"); intervalStr != "" {
		parsed, err := time.ParseDuration(intervalStr)
		if err != nil {
			return Config{}, fmt.Errorf("invalid check interval: %s", intervalStr)
		}
		if parsed <= 0 {
			return Config{}, fmt.Errorf("check interval must be positive: %s", intervalStr)
		}
		checkInterval = parsed
	}

	return Config{
		Platform:            platform,
		Enabled:             enabled,
		DefaultIdleDuration: defaultDuration,
		CheckInterval:       checkInterval,
	}, nil
}
