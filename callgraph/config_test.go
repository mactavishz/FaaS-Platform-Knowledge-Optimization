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
		smaKey      string
		smaValue    string
		emaKey      string
		emaValue    string
		expectError bool
		expected    bool
		method      AveragingMethod
		expSMA      int
		expEMA      float64
	}{
		{
			name:        "tinyfaas enabled",
			platform:    "tinyfaas",
			envKey:      "CALLGRAPH_ENABLED",
			envValue:    "true",
			method:      SimpleMovingAverage,
			expSMA:      DEFAULT_SMA_WINDOW_SIZE,
			expectError: false,
			expected:    true,
		},
		{
			name:        "tinyfaas enabled with 1",
			platform:    "tinyfaas",
			envKey:      "CALLGRAPH_ENABLED",
			envValue:    "1",
			method:      SimpleMovingAverage,
			expSMA:      DEFAULT_SMA_WINDOW_SIZE,
			expectError: false,
			expected:    true,
		},
		{
			name:        "tinyfaas disabled",
			platform:    "tinyfaas",
			envKey:      "CALLGRAPH_ENABLED",
			envValue:    "false",
			method:      SimpleMovingAverage,
			expSMA:      DEFAULT_SMA_WINDOW_SIZE,
			expectError: false,
			expected:    false,
		},
		{
			name:        "tinyfaas sma window from env",
			platform:    "tinyfaas",
			envKey:      "CALLGRAPH_ENABLED",
			envValue:    "true",
			smaKey:      "CALLGRAPH_SMA_WINDOW_SIZE",
			smaValue:    "25",
			method:      SimpleMovingAverage,
			expSMA:      25,
			expectError: false,
			expected:    true,
		},
		{
			name:        "tinyfaas invalid sma window falls back",
			platform:    "tinyfaas",
			envKey:      "CALLGRAPH_ENABLED",
			envValue:    "true",
			smaKey:      "CALLGRAPH_SMA_WINDOW_SIZE",
			smaValue:    "invalid",
			method:      SimpleMovingAverage,
			expSMA:      DEFAULT_SMA_WINDOW_SIZE,
			expectError: false,
			expected:    true,
		},
		{
			name:        "tinyfaas method ema uppercase",
			platform:    "tinyfaas",
			envKey:      "CALLGRAPH_ENABLED",
			envValue:    "true",
			methodKey:   "CALLGRAPH_METHOD",
			methodValue: "EMA",
			method:      ExponentialMovingAverage,
			expEMA:      DEFAULT_EMA_ALPHA,
			expectError: false,
			expected:    true,
		},
		{
			name:        "tinyfaas method ema mixed-case",
			platform:    "tinyfaas",
			envKey:      "CALLGRAPH_ENABLED",
			envValue:    "true",
			methodKey:   "CALLGRAPH_METHOD",
			methodValue: "eMa",
			emaKey:      "CALLGRAPH_EMA_ALPHA",
			emaValue:    "0.65",
			method:      ExponentialMovingAverage,
			expEMA:      0.65,
			expectError: false,
			expected:    true,
		},
		{
			name:        "tinyfaas invalid ema alpha falls back",
			platform:    "tinyfaas",
			envKey:      "CALLGRAPH_ENABLED",
			envValue:    "true",
			methodKey:   "CALLGRAPH_METHOD",
			methodValue: "EMA",
			emaKey:      "CALLGRAPH_EMA_ALPHA",
			emaValue:    "1.7",
			method:      ExponentialMovingAverage,
			expEMA:      DEFAULT_EMA_ALPHA,
			expectError: false,
			expected:    true,
		},
		{
			name:        "tinyfaas invalid method falls back to sma",
			platform:    "tinyfaas",
			envKey:      "CALLGRAPH_ENABLED",
			envValue:    "true",
			methodKey:   "CALLGRAPH_METHOD",
			methodValue: "WMA",
			method:      SimpleMovingAverage,
			expSMA:      DEFAULT_SMA_WINDOW_SIZE,
			expectError: false,
			expected:    true,
		},
		{
			name:        "faasd enabled",
			platform:    "faasd",
			envKey:      "CALLGRAPH_ENABLED",
			envValue:    "true",
			method:      SimpleMovingAverage,
			expSMA:      DEFAULT_SMA_WINDOW_SIZE,
			expectError: false,
			expected:    true,
		},
		{
			name:        "faasd sma window from env",
			platform:    "faasd",
			envKey:      "CALLGRAPH_ENABLED",
			envValue:    "true",
			smaKey:      "CALLGRAPH_SMA_WINDOW_SIZE",
			smaValue:    "30",
			method:      SimpleMovingAverage,
			expSMA:      30,
			expectError: false,
			expected:    true,
		},
		{
			name:        "faasd method sma lower-case",
			platform:    "faasd",
			envKey:      "CALLGRAPH_ENABLED",
			envValue:    "true",
			methodKey:   "CALLGRAPH_METHOD",
			methodValue: "sma",
			method:      SimpleMovingAverage,
			expSMA:      DEFAULT_SMA_WINDOW_SIZE,
			expectError: false,
			expected:    true,
		},
		{
			name:        "faasd method ema",
			platform:    "faasd",
			envKey:      "CALLGRAPH_ENABLED",
			envValue:    "true",
			methodKey:   "CALLGRAPH_METHOD",
			methodValue: "EMA",
			emaKey:      "CALLGRAPH_EMA_ALPHA",
			emaValue:    "0.2",
			method:      ExponentialMovingAverage,
			expEMA:      0.2,
			expectError: false,
			expected:    true,
		},
		{
			name:        "invalid platform",
			platform:    "invalid",
			envKey:      "",
			envValue:    "",
			method:      SimpleMovingAverage,
			expSMA:      DEFAULT_SMA_WINDOW_SIZE,
			expectError: true,
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear any existing env vars
			os.Unsetenv("CALLGRAPH_ENABLED")
			os.Unsetenv("CALLGRAPH_METHOD")
			os.Unsetenv("CALLGRAPH_SMA_WINDOW_SIZE")
			os.Unsetenv("CALLGRAPH_EMA_ALPHA")

			// Set the test env var
			if tt.envKey != "" {
				os.Setenv(tt.envKey, tt.envValue)
				defer os.Unsetenv(tt.envKey)
			}

			if tt.methodKey != "" {
				os.Setenv(tt.methodKey, tt.methodValue)
				defer os.Unsetenv(tt.methodKey)
			}

			if tt.smaKey != "" {
				os.Setenv(tt.smaKey, tt.smaValue)
				defer os.Unsetenv(tt.smaKey)
			}

			if tt.emaKey != "" {
				os.Setenv(tt.emaKey, tt.emaValue)
				defer os.Unsetenv(tt.emaKey)
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
				assert.InDelta(t, tt.expEMA, config.EMAConfig.Alpha, 0.000001)
			} else {
				assert.NotNil(t, config.SMAConfig)
				assert.Nil(t, config.EMAConfig)
				assert.Equal(t, tt.expSMA, config.SMAConfig.WindowSize)
			}

			assert.Equal(t, DefaultContextTTL, config.ContextTTL)
			assert.Equal(t, DefaultContextCleanupInterval, config.ContextCleanupInterval)
			assert.True(t, config.Prewarm.Enabled)
		})
	}
}

func TestNewConfigFromEnvDefaults(t *testing.T) {
	// Clear any existing env vars
	os.Unsetenv("CALLGRAPH_ENABLED")
	os.Unsetenv("CALLGRAPH_METHOD")
	os.Unsetenv("CALLGRAPH_SMA_WINDOW_SIZE")
	os.Unsetenv("CALLGRAPH_EMA_ALPHA")

	config, err := NewConfigFromEnv("tinyfaas")
	require.NoError(t, err)

	// When env var is not set, should be disabled
	assert.False(t, config.Enabled)
	assert.Equal(t, SimpleMovingAverage, config.Method)
	assert.NotNil(t, config.SMAConfig)
	assert.Equal(t, DEFAULT_SMA_WINDOW_SIZE, config.SMAConfig.WindowSize)
	assert.Nil(t, config.EMAConfig)

	// But prewarm config should still have defaults
	assert.True(t, config.Prewarm.Enabled)
}
