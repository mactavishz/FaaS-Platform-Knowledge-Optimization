package autoscaler

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const DEFAULT_IDLE_DURATION_MINUTES = 15
const DEFAULT_CHECK_INTERVAL_SECONDS = 10

// Config holds the autoscaler configuration
type Config struct {
	Platform            string
	Enabled             bool
	DefaultIdleDuration time.Duration
	CheckInterval       time.Duration
}

// NewConfigFromEnv loads autoscaler configuration from environment variables
// Supports both FAASD_ and TF_ prefixes for platform compatibility
func NewConfigFromEnv(platform string) (Config, error) {
	var enabledKey, durationKey string
	platform = strings.ToLower(platform)

	switch platform {
	case "faasd":
		enabledKey = "FAASD_AUTOSCALER_ENABLED"
		durationKey = "FAASD_DEFAULT_SCALE_TO_ZERO_IDLE_DURATION"
	case "tinyfaas":
		enabledKey = "TF_AUTOSCALER_ENABLED"
		durationKey = "TF_DEFAULT_SCALE_TO_ZERO_IDLE_DURATION"
	default:
		return Config{}, fmt.Errorf("invalid platform: %s", platform)
	}

	enabled := os.Getenv(enabledKey) == "true" || os.Getenv(enabledKey) == "1"

	defaultDuration := DEFAULT_IDLE_DURATION_MINUTES * time.Minute // Default: 15 minutes
	if durationStr := os.Getenv(durationKey); durationStr != "" {
		if parsed, err := time.ParseDuration(durationStr); err == nil {
			defaultDuration = parsed
		} else {
			return Config{}, fmt.Errorf("invalid duration: %s", durationStr)
		}
	}

	return Config{
		Platform:            platform,
		Enabled:             enabled,
		DefaultIdleDuration: defaultDuration,
		CheckInterval:       DEFAULT_CHECK_INTERVAL_SECONDS * time.Second, // Check every 10 seconds
	}, nil
}
