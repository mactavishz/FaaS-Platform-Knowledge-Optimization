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
		methodKey   string
		methodValue string
		expectError bool
		expected    bool
		method      AveragingMethod
	}{
		{
			name:        "tinyfaas enabled",
			platform:    "tinyfaas",
			envKey:      "TF_CALLGRAPH_ENABLED",
			envValue:    "true",
			method:      SimpleMovingAverage,
			expectError: false,
			expected:    true,
		},
		{
			name:        "tinyfaas enabled with 1",
			platform:    "tinyfaas",
			envKey:      "TF_CALLGRAPH_ENABLED",
			envValue:    "1",
			method:      SimpleMovingAverage,
			expectError: false,
			expected:    true,
		},
		{
			name:        "tinyfaas disabled",
			platform:    "tinyfaas",
			envKey:      "TF_CALLGRAPH_ENABLED",
			envValue:    "false",
			method:      SimpleMovingAverage,
			expectError: false,
			expected:    false,
		},
		{
			name:        "tinyfaas method ema uppercase",
			platform:    "tinyfaas",
			envKey:      "TF_CALLGRAPH_ENABLED",
			envValue:    "true",
			methodKey:   "TF_CALLGRAPH_METHOD",
			methodValue: "EMA",
			method:      ExponentialMovingAverage,
			expectError: false,
			expected:    true,
		},
		{
			name:        "tinyfaas method ema mixed-case",
			platform:    "tinyfaas",
			envKey:      "TF_CALLGRAPH_ENABLED",
			envValue:    "true",
			methodKey:   "TF_CALLGRAPH_METHOD",
			methodValue: "eMa",
			method:      ExponentialMovingAverage,
			expectError: false,
			expected:    true,
		},
		{
			name:        "tinyfaas invalid method falls back to sma",
			platform:    "tinyfaas",
			envKey:      "TF_CALLGRAPH_ENABLED",
			envValue:    "true",
			methodKey:   "TF_CALLGRAPH_METHOD",
			methodValue: "WMA",
			method:      SimpleMovingAverage,
			expectError: false,
			expected:    true,
		},
		{
			name:        "faasd enabled",
			platform:    "faasd",
			envKey:      "FAASD_CALLGRAPH_ENABLED",
			envValue:    "true",
			method:      SimpleMovingAverage,
			expectError: false,
			expected:    true,
		},
		{
			name:        "faasd method sma lower-case",
			platform:    "faasd",
			envKey:      "FAASD_CALLGRAPH_ENABLED",
			envValue:    "true",
			methodKey:   "FAASD_CALLGRAPH_METHOD",
			methodValue: "sma",
			method:      SimpleMovingAverage,
			expectError: false,
			expected:    true,
		},
		{
			name:        "faasd method ema",
			platform:    "faasd",
			envKey:      "FAASD_CALLGRAPH_ENABLED",
			envValue:    "true",
			methodKey:   "FAASD_CALLGRAPH_METHOD",
			methodValue: "EMA",
			method:      ExponentialMovingAverage,
			expectError: false,
			expected:    true,
		},
		{
			name:        "invalid platform",
			platform:    "invalid",
			envKey:      "",
			envValue:    "",
			method:      SimpleMovingAverage,
			expectError: true,
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear any existing env vars
			os.Unsetenv("TF_CALLGRAPH_ENABLED")
			os.Unsetenv("FAASD_CALLGRAPH_ENABLED")
			os.Unsetenv("TF_CALLGRAPH_METHOD")
			os.Unsetenv("FAASD_CALLGRAPH_METHOD")

			// Set the test env var
			if tt.envKey != "" {
				os.Setenv(tt.envKey, tt.envValue)
				defer os.Unsetenv(tt.envKey)
			}

			if tt.methodKey != "" {
				os.Setenv(tt.methodKey, tt.methodValue)
				defer os.Unsetenv(tt.methodKey)
			}

			config, err := NewConfigFromEnv(tt.platform)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, config.Enabled)
			assert.Equal(t, tt.method, config.Method)

			if config.Method == ExponentialMovingAverage {
				assert.NotNil(t, config.EMAConfig)
				assert.Nil(t, config.SMAConfig)
			} else {
				assert.NotNil(t, config.SMAConfig)
				assert.Nil(t, config.EMAConfig)
			}

			assert.Equal(t, DefaultContextTTL, config.ContextTTL)
			assert.Equal(t, DefaultContextCleanupInterval, config.ContextCleanupInterval)
			assert.True(t, config.Prewarm.Enabled)
		})
	}
}

func TestNewConfigFromEnvDefaults(t *testing.T) {
	// Clear any existing env vars
	os.Unsetenv("TF_CALLGRAPH_ENABLED")
	os.Unsetenv("TF_CALLGRAPH_METHOD")

	config, err := NewConfigFromEnv("tinyfaas")
	require.NoError(t, err)

	// When env var is not set, should be disabled
	assert.False(t, config.Enabled)
	assert.Equal(t, SimpleMovingAverage, config.Method)
	assert.NotNil(t, config.SMAConfig)
	assert.Nil(t, config.EMAConfig)

	// But prewarm config should still have defaults
	assert.True(t, config.Prewarm.Enabled)
}
