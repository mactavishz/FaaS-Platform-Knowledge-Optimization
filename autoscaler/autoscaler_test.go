package autoscaler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

// MockScaleOperation is a mock implementation of ScaleOperation
type MockScaleOperation struct {
	mock.Mock
}

func (m *MockScaleOperation) ScaleDown(functionName string) error {
	args := m.Called(functionName)
	return args.Error(0)
}

func (m *MockScaleOperation) ScaleUp(functionName string) error {
	args := m.Called(functionName)
	return args.Error(0)
}

func TestNew(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)
	config := Config{
		Enabled:       true,
		CheckInterval: 10 * time.Second,
	}

	as := New(config, mockOp, logger)

	assert.NotNil(t, as)
	assert.Equal(t, &config, as.config)
	assert.Equal(t, mockOp, as.scaleOperation)
	assert.NotNil(t, as.functions)
	assert.NotNil(t, as.stopChan)
	assert.NotNil(t, as.doneChan)
}

func TestRegisterFunction(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)
	config := Config{
		Enabled:             true,
		DefaultIdleDuration: 5 * time.Minute,
		Platform:            "faasd",
	}
	as := New(config, mockOp, logger)

	labels := map[string]string{
		"com.openfaas.scale.zero":               "true",
		"com.openfaas.scale.zero.idle_duration": "10m",
	}

	as.RegisterFunction("test-func", labels)

	as.functionsMutex.RLock()
	state, exists := as.functions["test-func"]
	as.functionsMutex.RUnlock()

	assert.True(t, exists)
	assert.Equal(t, "test-func", state.Name)
	assert.True(t, state.ScaleToZeroEnabled)
	assert.Equal(t, 10*time.Minute, state.IdleDuration)
	assert.False(t, state.IsScaledDown)
}

func TestUnregisterFunction(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)
	config := Config{Enabled: true}
	as := New(config, mockOp, logger)

	as.RegisterFunction("test-func", nil)
	as.UnregisterFunction("test-func")

	as.functionsMutex.RLock()
	_, exists := as.functions["test-func"]
	as.functionsMutex.RUnlock()

	assert.False(t, exists)
}

func TestRecordActivity(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)
	config := Config{Enabled: true}
	as := New(config, mockOp, logger)

	as.RegisterFunction("test-func", nil)

	as.functionsMutex.RLock()
	initialTime := as.functions["test-func"].LastAccessTime
	as.functionsMutex.RUnlock()

	// Ensure time advances
	time.Sleep(1 * time.Millisecond)

	as.RecordActivity("test-func")

	as.functionsMutex.RLock()
	newTime := as.functions["test-func"].LastAccessTime
	as.functionsMutex.RUnlock()

	assert.True(t, newTime.After(initialTime))
}

func TestRecordActivityBatch(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)
	config := Config{Enabled: true}
	as := New(config, mockOp, logger)

	as.RegisterFunction("func-a", nil)
	as.RegisterFunction("func-b", nil)
	as.RegisterFunction("func-c", nil)

	as.functionsMutex.RLock()
	initialTimeA := as.functions["func-a"].LastAccessTime
	initialTimeB := as.functions["func-b"].LastAccessTime
	initialTimeC := as.functions["func-c"].LastAccessTime
	as.functionsMutex.RUnlock()

	// Ensure time advances
	time.Sleep(1 * time.Millisecond)

	// Record batch activity for only func-a and func-c
	as.RecordActivityBatch([]string{"func-a", "func-c", "non-existent"})

	as.functionsMutex.RLock()
	newTimeA := as.functions["func-a"].LastAccessTime
	newTimeB := as.functions["func-b"].LastAccessTime
	newTimeC := as.functions["func-c"].LastAccessTime
	as.functionsMutex.RUnlock()

	// func-a and func-c should have updated times
	assert.True(t, newTimeA.After(initialTimeA), "func-a should have updated time")
	assert.True(t, newTimeC.After(initialTimeC), "func-c should have updated time")
	// func-b should not be updated
	assert.Equal(t, initialTimeB, newTimeB, "func-b should not be updated")
	// All updated functions should have the same time (batch uses single now)
	assert.Equal(t, newTimeA, newTimeC, "batch should use same timestamp for all functions")
}

func TestRecordActivityBatch_Disabled(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)
	config := Config{Enabled: false}
	as := New(config, mockOp, logger)

	// Should not panic or error when disabled
	as.RecordActivityBatch([]string{"func-a", "func-b"})
}

func TestRecordActivityBatch_Empty(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)
	config := Config{Enabled: true}
	as := New(config, mockOp, logger)

	// Should not panic or error with empty slice
	as.RecordActivityBatch([]string{})
	as.RecordActivityBatch(nil)
}

func TestCheckIdleFunctions_ScaleDown(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)
	config := Config{
		Enabled:             true,
		DefaultIdleDuration: 100 * time.Millisecond,
	}
	as := New(config, mockOp, logger)

	mockOp.On("ScaleDown", "test-func").Return(nil)

	as.RegisterFunction("test-func", nil)

	// Manually set LastAccessTime to simulate idle
	as.functionsMutex.Lock()
	as.functions["test-func"].LastAccessTime = time.Now().Add(-200 * time.Millisecond)
	as.functionsMutex.Unlock()

	as.checkIdleFunctions()

	mockOp.AssertExpectations(t)

	as.functionsMutex.RLock()
	state := as.functions["test-func"]
	as.functionsMutex.RUnlock()

	assert.True(t, state.IsScaledDown)
}

func TestCheckIdleFunctions_NotIdle(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)
	config := Config{
		Enabled:             true,
		DefaultIdleDuration: 1 * time.Hour,
	}
	as := New(config, mockOp, logger)

	as.RegisterFunction("test-func", nil)

	as.checkIdleFunctions()

	mockOp.AssertNotCalled(t, "ScaleDown", "test-func")

	as.functionsMutex.RLock()
	state := as.functions["test-func"]
	as.functionsMutex.RUnlock()

	assert.False(t, state.IsScaledDown)
}

func TestParseScaleConfig(t *testing.T) {
	tests := []struct {
		name            string
		platform        string
		labels          map[string]string
		defaultDuration time.Duration
		wantEnabled     bool
		wantDuration    time.Duration
	}{
		{
			name:            "faasd enabled with duration",
			platform:        "faasd",
			labels:          map[string]string{"com.openfaas.scale.zero": "true", "com.openfaas.scale.zero.idle_duration": "5m"},
			defaultDuration: 1 * time.Minute,
			wantEnabled:     true,
			wantDuration:    5 * time.Minute,
		},
		{
			name:            "faasd disabled",
			platform:        "faasd",
			labels:          map[string]string{"com.openfaas.scale.zero": "false"},
			defaultDuration: 1 * time.Minute,
			wantEnabled:     false,
			wantDuration:    1 * time.Minute,
		},
		{
			name:            "tinyfaas enabled with duration",
			platform:        "tinyfaas",
			labels:          map[string]string{"com.tinyfaas.scale.zero": "true", "com.tinyfaas.scale.zero.idle_duration": "10m"},
			defaultDuration: 1 * time.Minute,
			wantEnabled:     true,
			wantDuration:    10 * time.Minute,
		},
		{
			name:            "default values",
			platform:        "faasd",
			labels:          map[string]string{},
			defaultDuration: 1 * time.Minute,
			wantEnabled:     true, // Default is enabled if autoscaler is enabled
			wantDuration:    1 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zap.NewNop()
			mockOp := new(MockScaleOperation)
			config := Config{
				Enabled:  true,
				Platform: tt.platform,
			}
			as := New(config, mockOp, logger)

			enabled, duration := as.parseScaleConfig(tt.labels, tt.defaultDuration)
			assert.Equal(t, tt.wantEnabled, enabled)
			assert.Equal(t, tt.wantDuration, duration)
		})
	}
}

func TestMarkScaledDown(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)
	config := Config{Enabled: true}
	as := New(config, mockOp, logger)

	as.RegisterFunction("test-func", nil)

	as.MarkScaledDown("test-func", true)
	assert.True(t, as.IsScaledDown("test-func"))

	as.MarkScaledDown("test-func", false)
	assert.False(t, as.IsScaledDown("test-func"))
}

func TestMarkScalingDown(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)
	config := Config{Enabled: true}
	as := New(config, mockOp, logger)

	as.RegisterFunction("test-func", nil)

	as.MarkScalingDown("test-func", true)
	assert.True(t, as.IsScalingDown("test-func"))

	as.MarkScalingDown("test-func", false)
	assert.False(t, as.IsScalingDown("test-func"))
}

func TestGetFunctionStatus(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)
	config := Config{Enabled: true}
	as := New(config, mockOp, logger)

	as.RegisterFunction("func1", nil)
	as.RegisterFunction("func2", nil)

	status := as.GetFunctionStatus()
	assert.Len(t, status, 2)
	assert.Contains(t, status, "func1")
	assert.Contains(t, status, "func2")
}

func TestStartStop(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)
	config := Config{
		Enabled:       true,
		CheckInterval: 10 * time.Millisecond,
	}
	as := New(config, mockOp, logger)

	as.Start()
	time.Sleep(20 * time.Millisecond) // Allow monitor to start
	as.Stop()

	// Ensure Stop completes without hanging
	select {
	case <-time.After(1 * time.Second):
		t.Fatal("Stop() didn't complete within timeout")
	default:
		// Success
	}
}

func TestStartStop_Disabled(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)
	config := Config{
		Enabled:       false,
		CheckInterval: 10 * time.Millisecond,
	}
	as := New(config, mockOp, logger)

	// Should not panic or block when disabled
	as.Start()
	as.Stop()
}

func TestScaleDownWrapper(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)
	config := Config{Enabled: true}
	as := New(config, mockOp, logger)

	mockOp.On("ScaleDown", "test-func").Return(nil)

	err := as.ScaleDown("test-func")
	assert.NoError(t, err)
	mockOp.AssertExpectations(t)
}

func TestScaleUpWrapper(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)
	config := Config{Enabled: true}
	as := New(config, mockOp, logger)

	mockOp.On("ScaleUp", "test-func").Return(nil)

	err := as.ScaleUp("test-func")
	assert.NoError(t, err)
	mockOp.AssertExpectations(t)
}

func TestIsEnabled(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)

	t.Run("enabled", func(t *testing.T) {
		config := Config{Enabled: true}
		as := New(config, mockOp, logger)
		assert.True(t, as.IsEnabled())
	})

	t.Run("disabled", func(t *testing.T) {
		config := Config{Enabled: false}
		as := New(config, mockOp, logger)
		assert.False(t, as.IsEnabled())
	})
}

func TestGetIdleTime(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)
	config := Config{Enabled: true}
	as := New(config, mockOp, logger)

	as.RegisterFunction("test-func", nil)

	// Set a known last access time
	as.functionsMutex.Lock()
	as.functions["test-func"].LastAccessTime = time.Now().Add(-5 * time.Minute)
	as.functionsMutex.Unlock()

	idleTime := as.GetIdleTime("test-func")
	assert.True(t, idleTime >= 5*time.Minute, "idle time should be at least 5 minutes")
	assert.True(t, idleTime < 6*time.Minute, "idle time should be less than 6 minutes")
}

func TestGetIdleTime_NonExistent(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)
	config := Config{Enabled: true}
	as := New(config, mockOp, logger)

	idleTime := as.GetIdleTime("non-existent")
	assert.Equal(t, time.Duration(0), idleTime)
}

func TestCheckIdleFunctions_AlreadyScaledDown(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)
	config := Config{
		Enabled:             true,
		DefaultIdleDuration: 100 * time.Millisecond,
	}
	as := New(config, mockOp, logger)

	as.RegisterFunction("test-func", nil)

	// Mark as already scaled down
	as.MarkScaledDown("test-func", true)

	// Set as idle
	as.functionsMutex.Lock()
	as.functions["test-func"].LastAccessTime = time.Now().Add(-200 * time.Millisecond)
	as.functionsMutex.Unlock()

	as.checkIdleFunctions()

	// Should not call ScaleDown again
	mockOp.AssertNotCalled(t, "ScaleDown", "test-func")
}

func TestCheckIdleFunctions_ScaleToZeroDisabled(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)
	config := Config{
		Enabled:             true,
		DefaultIdleDuration: 100 * time.Millisecond,
		Platform:            "faasd",
	}
	as := New(config, mockOp, logger)

	labels := map[string]string{
		"com.openfaas.scale.zero": "false",
	}
	as.RegisterFunction("test-func", labels)

	// Set as idle
	as.functionsMutex.Lock()
	as.functions["test-func"].LastAccessTime = time.Now().Add(-200 * time.Millisecond)
	as.functionsMutex.Unlock()

	as.checkIdleFunctions()

	// Should not scale down when scale-to-zero is disabled
	mockOp.AssertNotCalled(t, "ScaleDown", "test-func")
}

func TestCheckIdleFunctions_WithError(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)
	config := Config{
		Enabled:             true,
		DefaultIdleDuration: 100 * time.Millisecond,
	}
	as := New(config, mockOp, logger)

	mockOp.On("ScaleDown", "test-func").Return(assert.AnError)

	as.RegisterFunction("test-func", nil)

	// Set as idle
	as.functionsMutex.Lock()
	as.functions["test-func"].LastAccessTime = time.Now().Add(-200 * time.Millisecond)
	as.functionsMutex.Unlock()

	as.checkIdleFunctions()

	mockOp.AssertExpectations(t)

	// Should remain not scaled down due to error
	assert.False(t, as.IsScaledDown("test-func"))
}

func TestCheckIdleFunctions_MultipleFunctions(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)
	config := Config{
		Enabled:             true,
		DefaultIdleDuration: 100 * time.Millisecond,
	}
	as := New(config, mockOp, logger)

	mockOp.On("ScaleDown", "idle-func").Return(nil)

	// Register multiple functions
	as.RegisterFunction("idle-func", nil)
	as.RegisterFunction("active-func", nil)

	// Set one as idle, one as active
	as.functionsMutex.Lock()
	as.functions["idle-func"].LastAccessTime = time.Now().Add(-200 * time.Millisecond)
	as.functions["active-func"].LastAccessTime = time.Now()
	as.functionsMutex.Unlock()

	as.checkIdleFunctions()

	mockOp.AssertExpectations(t)
	assert.True(t, as.IsScaledDown("idle-func"))
	assert.False(t, as.IsScaledDown("active-func"))
}

func TestDisabledAutoscaler(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)
	config := Config{Enabled: false}
	as := New(config, mockOp, logger)

	// None of these should panic or do anything when disabled
	as.RegisterFunction("test-func", nil)
	as.RecordActivity("test-func")
	as.UnregisterFunction("test-func")
	as.MarkScaledDown("test-func", true)
	as.MarkScalingDown("test-func", true)

	assert.False(t, as.IsScaledDown("test-func"))
	assert.False(t, as.IsScalingDown("test-func"))
}

func TestIsScaledDown_NonExistent(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)
	config := Config{Enabled: true}
	as := New(config, mockOp, logger)

	assert.False(t, as.IsScaledDown("non-existent"))
}

func TestIsScalingDown_NonExistent(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)
	config := Config{Enabled: true}
	as := New(config, mockOp, logger)

	assert.False(t, as.IsScalingDown("non-existent"))
}

func TestRecordActivity_NonExistent(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)
	config := Config{Enabled: true}
	as := New(config, mockOp, logger)

	// Should not panic when recording activity for non-existent function
	as.RecordActivity("non-existent")
}

func TestDefaultCheckInterval(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)
	config := Config{
		Enabled:       true,
		CheckInterval: 0, // No interval set
	}

	as := New(config, mockOp, logger)

	assert.Equal(t, DEFAULT_CHECK_INTERVAL_SECONDS*time.Second, as.config.CheckInterval)
}

func TestParseScaleConfig_InvalidDuration(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)
	config := Config{
		Enabled:  true,
		Platform: "faasd",
	}
	as := New(config, mockOp, logger)

	labels := map[string]string{
		"com.openfaas.scale.zero":               "true",
		"com.openfaas.scale.zero.idle_duration": "invalid-duration",
	}

	defaultDuration := 5 * time.Minute
	enabled, duration := as.parseScaleConfig(labels, defaultDuration)

	assert.True(t, enabled)
	assert.Equal(t, defaultDuration, duration, "should fall back to default on invalid duration")
}

func TestConcurrentAccess(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)
	config := Config{
		Enabled:             true,
		DefaultIdleDuration: 1 * time.Hour,
	}
	as := New(config, mockOp, logger)

	// Test concurrent registration and activity recording
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			funcName := "func-" + string(rune('0'+idx))
			as.RegisterFunction(funcName, nil)
			for j := 0; j < 100; j++ {
				as.RecordActivity(funcName)
				_ = as.IsScaledDown(funcName)
				_ = as.GetIdleTime(funcName)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	status := as.GetFunctionStatus()
	assert.Len(t, status, 10)
}

func TestMonitorLoop_Integration(t *testing.T) {
	logger := zap.NewNop()
	mockOp := new(MockScaleOperation)
	config := Config{
		Enabled:             true,
		CheckInterval:       50 * time.Millisecond,
		DefaultIdleDuration: 100 * time.Millisecond,
	}
	as := New(config, mockOp, logger)

	mockOp.On("ScaleDown", "test-func").Return(nil)

	as.RegisterFunction("test-func", nil)

	// Set function as idle
	as.functionsMutex.Lock()
	as.functions["test-func"].LastAccessTime = time.Now().Add(-200 * time.Millisecond)
	as.functionsMutex.Unlock()

	as.Start()
	defer as.Stop()

	// Wait for monitor to detect and scale down
	time.Sleep(150 * time.Millisecond)

	mockOp.AssertExpectations(t)
	assert.True(t, as.IsScaledDown("test-func"))
}
