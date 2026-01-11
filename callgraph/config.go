package callgraph

import (
	"fmt"
	"os"
	"strings"
)

// NewConfigFromEnv loads callgraph configuration from environment variables
// Supports both FAASD_ and TF_ prefixes for platform compatibility
func NewConfigFromEnv(platform string) (Config, error) {
	var enabledKey string
	platform = strings.ToLower(platform)

	switch platform {
	case "faasd":
		enabledKey = "FAASD_CALLGRAPH_ENABLED"
	case "tinyfaas":
		enabledKey = "TF_CALLGRAPH_ENABLED"
	default:
		return Config{}, fmt.Errorf("invalid platform: %s", platform)
	}

	enabled := os.Getenv(enabledKey) == "true" || os.Getenv(enabledKey) == "1"
	config := DefaultConfig()
	config.Enabled = enabled
	return *config, nil
}
