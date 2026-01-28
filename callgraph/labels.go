package callgraph

import (
	"strings"
)

// ParseCallgraphConfig parses per-function callgraph enablement from labels.
func ParseCallgraphConfig(labels map[string]string, platform string, tracker Tracker) bool {
	// If tracker is disabled, per-function cannot enable it
	if tracker == nil || !tracker.Enabled() {
		return false
	}

	if labels == nil {
		return true
	}

	var key string
	switch strings.ToLower(platform) {
	case "faasd":
		key = "com.openfaas.callgraph.enabled"
	case "tinyfaas":
		fallthrough
	default:
		key = "com.tinyfaas.callgraph.enabled"
	}

	raw, ok := labels[key]
	if !ok {
		return true
	}

	parsed, valid := parseBoolLabel(raw)
	if !valid {
		return true
	}

	return parsed
}

// parseBoolLabel parses a string as a boolean label value.
// Valid values: "true", "1" → true; "false", "0" → false.
func parseBoolLabel(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1":
		return true, true
	case "false", "0":
		return false, true
	default:
		return false, false
	}
}
