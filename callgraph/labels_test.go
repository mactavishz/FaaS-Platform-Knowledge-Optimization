package callgraph

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseCallgraphConfig(t *testing.T) {

	tests := []struct {
		name     string
		labels   map[string]string
		platform string
		tracker  Tracker
		expected bool
	}{
		// Master switch: global disabled always returns false
		{
			name:     "global disabled, label true",
			labels:   map[string]string{"com.tinyfaas.callgraph.enabled": "true"},
			platform: "tinyfaas",
			tracker:  New(WithConfig(&Config{Enabled: false, ContextTTL: 1, ContextCleanupInterval: 1})),
			expected: false,
		},
		{
			name:     "global disabled, label false",
			labels:   map[string]string{"com.tinyfaas.callgraph.enabled": "false"},
			platform: "tinyfaas",
			tracker:  New(WithConfig(&Config{Enabled: false, ContextTTL: 1, ContextCleanupInterval: 1})),
			expected: false,
		},
		{
			name:     "global disabled, no label",
			labels:   nil,
			platform: "tinyfaas",
			tracker:  New(WithConfig(&Config{Enabled: false, ContextTTL: 1, ContextCleanupInterval: 1})),
			expected: false,
		},
		// tinyFaaS platform
		{
			name:     "tinyfaas enabled, label true",
			labels:   map[string]string{"com.tinyfaas.callgraph.enabled": "true"},
			platform: "tinyfaas",
			tracker:  New(),
			expected: true,
		},
		{
			name:     "tinyfaas enabled, label 1",
			labels:   map[string]string{"com.tinyfaas.callgraph.enabled": "1"},
			platform: "tinyfaas",
			tracker:  New(),
			expected: true,
		},
		{
			name:     "tinyfaas enabled, label false",
			labels:   map[string]string{"com.tinyfaas.callgraph.enabled": "false"},
			platform: "tinyfaas",
			tracker:  New(),
			expected: false,
		},
		{
			name:     "tinyfaas enabled, label 0",
			labels:   map[string]string{"com.tinyfaas.callgraph.enabled": "0"},
			platform: "tinyfaas",
			tracker:  New(),
			expected: false,
		},
		{
			name:     "tinyfaas enabled, no label (default enabled)",
			labels:   nil,
			platform: "tinyfaas",
			tracker:  New(),
			expected: true,
		},
		{
			name:     "tinyfaas enabled, empty labels map (default enabled)",
			labels:   map[string]string{},
			platform: "tinyfaas",
			tracker:  New(),
			expected: true,
		},
		// faasd platform
		{
			name:     "faasd enabled, label true",
			labels:   map[string]string{"com.openfaas.callgraph.enabled": "true"},
			platform: "faasd",
			tracker:  New(),
			expected: true,
		},
		{
			name:     "faasd enabled, label false",
			labels:   map[string]string{"com.openfaas.callgraph.enabled": "false"},
			platform: "faasd",
			tracker:  New(),
			expected: false,
		},
		{
			name:     "faasd enabled, no label (default enabled)",
			labels:   nil,
			platform: "faasd",
			tracker:  New(),
			expected: true,
		},
		// Invalid label values fall back to default (enabled)
		{
			name:     "tinyfaas enabled, invalid label value",
			labels:   map[string]string{"com.tinyfaas.callgraph.enabled": "invalid"},
			platform: "tinyfaas",
			tracker:  New(),
			expected: true, // Falls back to default (enabled)
		},
		{
			name:     "tinyfaas enabled, empty label value",
			labels:   map[string]string{"com.tinyfaas.callgraph.enabled": ""},
			platform: "tinyfaas",
			tracker:  New(),
			expected: true, // Falls back to default (enabled)
		},
		// Case insensitivity and whitespace handling
		{
			name:     "tinyfaas enabled, label TRUE (uppercase)",
			labels:   map[string]string{"com.tinyfaas.callgraph.enabled": "TRUE"},
			platform: "tinyfaas",
			tracker:  New(),
			expected: true,
		},
		{
			name:     "tinyfaas enabled, label with whitespace",
			labels:   map[string]string{"com.tinyfaas.callgraph.enabled": " true "},
			platform: "tinyfaas",
			tracker:  New(),
			expected: true,
		},
		// Wrong platform label key is ignored (falls back to default)
		{
			name:     "tinyfaas enabled, faasd label key (ignored)",
			labels:   map[string]string{"com.openfaas.callgraph.enabled": "false"},
			platform: "tinyfaas",
			tracker:  New(),
			expected: true, // Falls back to default since tinyfaas key not found
		},
		{
			name:     "faasd enabled, tinyfaas label key (ignored)",
			labels:   map[string]string{"com.tinyfaas.callgraph.enabled": "false"},
			platform: "faasd",
			tracker:  New(),
			expected: true, // Falls back to default since faasd key not found
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseCallgraphConfig(tt.labels, tt.platform, tt.tracker)
			assert.Equal(t, tt.expected, result, "ParseCallgraphConfig(%v, %q, %v)", tt.labels, tt.platform, tt.tracker.Enabled)
		})
	}
}

func TestParseBoolLabel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
		valid    bool
	}{
		{
			name:     "true",
			input:    "true",
			expected: true,
			valid:    true,
		},
		{
			name:     "1",
			input:    "1",
			expected: true,
			valid:    true,
		},
		{
			name:     "false",
			input:    "false",
			expected: false,
			valid:    true,
		},
		{
			name:     "0",
			input:    "0",
			expected: false,
			valid:    true,
		},
		{
			name:     "invalid value",
			input:    "invalid",
			expected: false,
			valid:    false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
			valid:    false,
		},
		{
			name:     "uppercase TRUE",
			input:    "TRUE",
			expected: true,
			valid:    true,
		},
		{
			name:     "with whitespace",
			input:    " true ",
			expected: true,
			valid:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, valid := parseBoolLabel(tt.input)
			assert.Equal(t, tt.expected, result)
			assert.Equal(t, tt.valid, valid)
		})
	}
}
