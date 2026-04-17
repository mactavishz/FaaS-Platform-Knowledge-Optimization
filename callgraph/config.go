package callgraph

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// NewConfigFromEnv loads callgraph configuration from environment variables.
// tinyfaas uses unprefixed names, faasd keeps FAASD_ names.
func NewConfigFromEnv(platform string) (Config, error) {
	var enabledKey, methodKey, smaWindowSizeKey, emaAlphaKey string
	platform = strings.ToLower(platform)

	switch platform {
	case "faasd":
		enabledKey = "FAASD_CALLGRAPH_ENABLED"
		methodKey = "FAASD_CALLGRAPH_METHOD"
		smaWindowSizeKey = "FAASD_CALLGRAPH_SMA_WINDOW_SIZE"
		emaAlphaKey = "FAASD_CALLGRAPH_EMA_ALPHA"
	case "tinyfaas":
		enabledKey = "CALLGRAPH_ENABLED"
		methodKey = "CALLGRAPH_METHOD"
		smaWindowSizeKey = "CALLGRAPH_SMA_WINDOW_SIZE"
		emaAlphaKey = "CALLGRAPH_EMA_ALPHA"
	default:
		return Config{}, fmt.Errorf("invalid platform: %s", platform)
	}

	enabled := os.Getenv(enabledKey) == "true" || os.Getenv(enabledKey) == "1"
	method := parseAveragingMethod(os.Getenv(methodKey))

	config := DefaultConfig()
	config.Enabled = enabled
	config.Method = method

	if method == ExponentialMovingAverage {
		config.EMAConfig = &EMAConfig{Alpha: parseEMAAlpha(os.Getenv(emaAlphaKey))}
		config.SMAConfig = nil
	} else {
		config.SMAConfig = &SMAConfig{WindowSize: parseSMAWindowSize(os.Getenv(smaWindowSizeKey))}
		config.EMAConfig = nil
	}

	return *config, nil
}

func parseSMAWindowSize(value string) int {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return DEFAULT_SMA_WINDOW_SIZE
	}

	windowSize, err := strconv.Atoi(trimmed)
	if err != nil || windowSize <= 0 {
		return DEFAULT_SMA_WINDOW_SIZE
	}

	return windowSize
}

func parseEMAAlpha(value string) float64 {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return DEFAULT_EMA_ALPHA
	}

	alpha, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || alpha <= 0 || alpha > 1 {
		return DEFAULT_EMA_ALPHA
	}

	return alpha
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
