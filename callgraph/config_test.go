package callgraph

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewConfigFromEnv(t *testing.T) {
	tests := []struct {
		name        string
		platform    string
		envKey      string
		envValue    string
		expectError bool
		expected    bool
	}{
		{
			name:        "tinyfaas enabled",
			platform:    "tinyfaas",
			envKey:      "TF_CALLGRAPH_ENABLED",
			envValue:    "true",
			expectError: false,
			expected:    true,
		},
		{
			name:        "tinyfaas enabled with 1",
			platform:    "tinyfaas",
			envKey:      "TF_CALLGRAPH_ENABLED",
			envValue:    "1",
			expectError: false,
			expected:    true,
		},
		{
			name:        "tinyfaas disabled",
			platform:    "tinyfaas",
			envKey:      "TF_CALLGRAPH_ENABLED",
			envValue:    "false",
			expectError: false,
			expected:    false,
		},
		{
			name:        "faasd enabled",
			platform:    "faasd",
			envKey:      "FAASD_CALLGRAPH_ENABLED",
			envValue:    "true",
			expectError: false,
			expected:    true,
		},
		{
			name:        "invalid platform",
			platform:    "invalid",
			envKey:      "",
			envValue:    "",
			expectError: true,
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear any existing env vars
			os.Unsetenv("TF_CALLGRAPH_ENABLED")
			os.Unsetenv("FAASD_CALLGRAPH_ENABLED")

			// Set the test env var
			if tt.envKey != "" {
				os.Setenv(tt.envKey, tt.envValue)
				defer os.Unsetenv(tt.envKey)
			}

			config, err := NewConfigFromEnv(tt.platform)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, config.Enabled)
			assert.Equal(t, DefaultContextTTL, config.ContextTTL)
			assert.Equal(t, DefaultContextCleanupInterval, config.ContextCleanupInterval)
			assert.True(t, config.Prewarm.Enabled)
			assert.Equal(t, 1, config.Prewarm.MinSamples)
		})
	}
}

func TestNewConfigFromEnvDefaults(t *testing.T) {
	// Clear any existing env vars
	os.Unsetenv("TF_CALLGRAPH_ENABLED")

	config, err := NewConfigFromEnv("tinyfaas")
	require.NoError(t, err)

	// When env var is not set, should be disabled
	assert.False(t, config.Enabled)

	// But prewarm config should still have defaults
	assert.True(t, config.Prewarm.Enabled)
}
