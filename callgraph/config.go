package callgraph

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// NewConfigFromEnv loads callgraph configuration from environment variables.
func NewConfigFromEnv(platform string) (Config, error) {
	platform = strings.ToLower(platform)

	switch platform {
	case "faasd", "tinyfaas":
	default:
		return Config{}, fmt.Errorf("invalid platform: %s", platform)
	}

	enabled := os.Getenv("CALLGRAPH_ENABLED") == "true" || os.Getenv("CALLGRAPH_ENABLED") == "1"
	prewarmEnabled := os.Getenv("CALLGRAPH_PREWARM_ENABLED") == "true" || os.Getenv("CALLGRAPH_PREWARM_ENABLED") == "1"
	method := parseAveragingMethod(os.Getenv("CALLGRAPH_METHOD"))

	config := DefaultConfig()
	config.Enabled = enabled
	config.Prewarm.Enabled = prewarmEnabled
	config.Method = method

	if method == ExponentialMovingAverage {
		config.EMAConfig = &EMAConfig{Alpha: parseEMAAlpha(os.Getenv("CALLGRAPH_EMA_ALPHA"))}
		config.SMAConfig = nil
	} else {
		config.SMAConfig = &SMAConfig{WindowSize: parseSMAWindowSize(os.Getenv("CALLGRAPH_SMA_WINDOW_SIZE"))}
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
