package callgraph

import (
	"fmt"
	"os"
	"strings"
)

// NewConfigFromEnv loads callgraph configuration from environment variables
// Supports both FAASD_ and TF_ prefixes for platform compatibility
func NewConfigFromEnv(platform string) (Config, error) {
	var enabledKey, methodKey string
	platform = strings.ToLower(platform)

	switch platform {
	case "faasd":
		enabledKey = "FAASD_CALLGRAPH_ENABLED"
		methodKey = "FAASD_CALLGRAPH_METHOD"
	case "tinyfaas":
		enabledKey = "TF_CALLGRAPH_ENABLED"
		methodKey = "TF_CALLGRAPH_METHOD"
	default:
		return Config{}, fmt.Errorf("invalid platform: %s", platform)
	}

	enabled := os.Getenv(enabledKey) == "true" || os.Getenv(enabledKey) == "1"
	method := parseAveragingMethod(os.Getenv(methodKey))

	config := DefaultConfig()
	config.Enabled = enabled
	config.Method = method

	if method == ExponentialMovingAverage {
		config.EMAConfig = &EMAConfig{Alpha: DEFAULT_EMA_ALPHA}
		config.SMAConfig = nil
	} else {
		config.SMAConfig = &SMAConfig{WindowSize: DEFAULT_SMA_WINDOW_SIZE}
		config.EMAConfig = nil
	}

	return *config, nil
}

func parseAveragingMethod(value string) AveragingMethod {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "EMA":
		return ExponentialMovingAverage
	case "SMA":
		fallthrough
	default:
		return SimpleMovingAverage
	}
}
