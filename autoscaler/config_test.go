package autoscaler

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewConfigFromEnv(t *testing.T) {
	tests := []struct {
		name            string
		platform        string
		enabledValue    string
		durationValue   string
		expectError     bool
		expectedEnabled bool
		expectedIdle    time.Duration
	}{
		{
			name:            "tinyfaas enabled true",
			platform:        "tinyfaas",
			enabledValue:    "true",
			expectedEnabled: true,
			expectedIdle:    DEFAULT_IDLE_DURATION_MINUTES * time.Minute,
		},
		{
			name:            "faasd enabled with 1",
			platform:        "faasd",
			enabledValue:    "1",
			expectedEnabled: true,
			expectedIdle:    DEFAULT_IDLE_DURATION_MINUTES * time.Minute,
		},
		{
			name:            "faasd disabled",
			platform:        "faasd",
			enabledValue:    "false",
			expectedEnabled: false,
			expectedIdle:    DEFAULT_IDLE_DURATION_MINUTES * time.Minute,
		},
		{
			name:            "tinyfaas custom duration",
			platform:        "tinyfaas",
			enabledValue:    "true",
			durationValue:   "45s",
			expectedEnabled: true,
			expectedIdle:    45 * time.Second,
		},
		{
			name:          "invalid duration returns error",
			platform:      "tinyfaas",
			enabledValue:  "true",
			durationValue: "invalid",
			expectError:   true,
		},
		{
			name:        "invalid platform",
			platform:    "invalid",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Unsetenv("AUTOSCALER_ENABLED")
			os.Unsetenv("DEFAULT_SCALE_TO_ZERO_IDLE_DURATION")

			if tt.enabledValue != "" {
				require.NoError(t, os.Setenv("AUTOSCALER_ENABLED", tt.enabledValue))
				t.Cleanup(func() { os.Unsetenv("AUTOSCALER_ENABLED") })
			}

			if tt.durationValue != "" {
				require.NoError(t, os.Setenv("DEFAULT_SCALE_TO_ZERO_IDLE_DURATION", tt.durationValue))
				t.Cleanup(func() { os.Unsetenv("DEFAULT_SCALE_TO_ZERO_IDLE_DURATION") })
			}

			config, err := NewConfigFromEnv(tt.platform)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.platform, config.Platform)
			assert.Equal(t, tt.expectedEnabled, config.Enabled)
			assert.Equal(t, tt.expectedIdle, config.DefaultIdleDuration)
			assert.Equal(t, DEFAULT_CHECK_INTERVAL_SECONDS*time.Second, config.CheckInterval)
		})
	}
}

func TestNewConfigFromEnvDefaults(t *testing.T) {
	os.Unsetenv("AUTOSCALER_ENABLED")
	os.Unsetenv("DEFAULT_SCALE_TO_ZERO_IDLE_DURATION")

	config, err := NewConfigFromEnv("tinyfaas")
	require.NoError(t, err)

	assert.False(t, config.Enabled)
	assert.Equal(t, DEFAULT_IDLE_DURATION_MINUTES*time.Minute, config.DefaultIdleDuration)
	assert.Equal(t, DEFAULT_CHECK_INTERVAL_SECONDS*time.Second, config.CheckInterval)
}

func TestNewConfigFromEnvCheckInterval(t *testing.T) {
	t.Run("defaults when unset", func(t *testing.T) {
		os.Unsetenv("AUTOSCALER_CHECK_INTERVAL")
		config, err := NewConfigFromEnv("tinyfaas")
		require.NoError(t, err)
		assert.Equal(t, DEFAULT_CHECK_INTERVAL_SECONDS*time.Second, config.CheckInterval)
	})

	t.Run("parses a valid override", func(t *testing.T) {
		t.Setenv("AUTOSCALER_CHECK_INTERVAL", "2s")
		config, err := NewConfigFromEnv("tinyfaas")
		require.NoError(t, err)
		assert.Equal(t, 2*time.Second, config.CheckInterval)
	})

	t.Run("rejects an unparseable value", func(t *testing.T) {
		t.Setenv("AUTOSCALER_CHECK_INTERVAL", "soon")
		_, err := NewConfigFromEnv("tinyfaas")
		require.Error(t, err)
	})

	t.Run("rejects a non-positive value", func(t *testing.T) {
		t.Setenv("AUTOSCALER_CHECK_INTERVAL", "0s")
		_, err := NewConfigFromEnv("tinyfaas")
		require.Error(t, err)
	})
}
