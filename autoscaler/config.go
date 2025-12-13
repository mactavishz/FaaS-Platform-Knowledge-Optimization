package autoscaler

import (
	"log"
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
func NewConfigFromEnv(platform string) Config {
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
		log.Printf("autoscaler: unknown platform %q, autoscaling disabled", platform)
		return Config{Enabled: false, Platform: platform}
	}

	enabled := os.Getenv(enabledKey) == "true" || os.Getenv(enabledKey) == "1"

	defaultDuration := DEFAULT_IDLE_DURATION_MINUTES * time.Minute // Default: 15 minutes
	if durationStr := os.Getenv(durationKey); durationStr != "" {
		if parsed, err := time.ParseDuration(durationStr); err == nil {
			defaultDuration = parsed
		} else {
			log.Printf("autoscaler: invalid %s value %q, using default %dm: %v",
				durationKey, durationStr, DEFAULT_IDLE_DURATION_MINUTES, err)
		}
	}

	if enabled {
		log.Printf("autoscaler: enabled for %s (default_idle_duration: %v)", platform, defaultDuration)
	} else {
		log.Printf("autoscaler: disabled for %s", platform)
	}

	return Config{
		Platform:            platform,
		Enabled:             enabled,
		DefaultIdleDuration: defaultDuration,
		CheckInterval:       DEFAULT_CHECK_INTERVAL_SECONDS * time.Second, // Check every 10 seconds
	}
}
