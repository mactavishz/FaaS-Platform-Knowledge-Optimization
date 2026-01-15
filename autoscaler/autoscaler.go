package autoscaler

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

// ScaleOperation defines the interface for scaling operations
type ScaleOperation interface {
	// ScaleDown stops the function containers
	ScaleDown(functionName string) error
	// ScaleUp starts the function containers
	ScaleUp(functionName string) error
}

// AutoScaler manages automatic scaling of functions based on activity
type AutoScaler struct {
	config *Config

	// Track function activity
	functions      map[string]*FunctionState
	functionsMutex sync.RWMutex

	// Callbacks for scaling operations
	scaleOperation ScaleOperation

	// Monitor goroutine control
	stopChan chan struct{}
	doneChan chan struct{}
	logger   *zap.Logger
}

// FunctionState tracks the state and activity of a function
type FunctionState struct {
	Name               string
	LastAccessTime     time.Time
	IdleDuration       time.Duration
	ScaleToZeroEnabled bool
	IsScaledDown       bool
	IsScalingDown      bool
	Labels             map[string]string
}

// New creates a new AutoScaler instance
func New(config Config, scaleOp ScaleOperation, logger *zap.Logger) *AutoScaler {
	if config.CheckInterval == 0 {
		config.CheckInterval = DEFAULT_CHECK_INTERVAL_SECONDS * time.Second
	}

	return &AutoScaler{
		config:         &config,
		functions:      make(map[string]*FunctionState),
		scaleOperation: scaleOp,
		stopChan:       make(chan struct{}),
		doneChan:       make(chan struct{}),
		logger:         logger,
	}
}

// Start begins the autoscaler monitoring loop
func (as *AutoScaler) Start() {
	if !as.config.Enabled {
		as.logger.Info("autoscaler disabled, not starting monitor")
		return
	}

	as.logger.Info("starting monitor loop")
	go as.startMonitor()
}

// Stop gracefully shuts down the autoscaler
func (as *AutoScaler) Stop() {
	if !as.config.Enabled {
		return
	}

	as.logger.Info("stopping monitor loop")
	close(as.stopChan)
	<-as.doneChan
	as.logger.Info("monitor loop stopped")
}

// RegisterFunction registers a function with the autoscaler
func (as *AutoScaler) RegisterFunction(name string, labels map[string]string) {
	if !as.config.Enabled {
		return
	}

	as.functionsMutex.Lock()
	defer as.functionsMutex.Unlock()

	// Parse scale-to-zero configuration from labels
	scaleToZeroEnabled, idleDuration := as.parseScaleConfig(labels, as.config.DefaultIdleDuration)

	state := &FunctionState{
		Name:               name,
		LastAccessTime:     time.Now(),
		IdleDuration:       idleDuration,
		ScaleToZeroEnabled: scaleToZeroEnabled,
		IsScaledDown:       false,
		Labels:             labels,
	}

	as.functions[name] = state
	as.logger.Info("registered function", zap.String("function", name),
		zap.Bool("scale_to_zero", scaleToZeroEnabled),
		zap.Duration("idle_duration", idleDuration))
}

// UnregisterFunction removes a function from the autoscaler
func (as *AutoScaler) UnregisterFunction(name string) {
	if !as.config.Enabled {
		return
	}

	as.functionsMutex.Lock()
	defer as.functionsMutex.Unlock()

	delete(as.functions, name)
	as.logger.Info("unregistered function", zap.String("function", name))
}

// ScaleDown is a wrapper for the ScaleDown operation of the ScaleOperation interface
func (as *AutoScaler) ScaleDown(functionName string) error {
	return as.scaleOperation.ScaleDown(functionName)
}

// ScaleUp is a wrapper for the ScaleUp operation of the ScaleOperation interface
func (as *AutoScaler) ScaleUp(functionName string) error {
	return as.scaleOperation.ScaleUp(functionName)
}

// RecordActivity records an invocation of a function
func (as *AutoScaler) RecordActivity(name string) {
	if !as.config.Enabled {
		return
	}

	as.functionsMutex.Lock()
	defer as.functionsMutex.Unlock()

	if state, exists := as.functions[name]; exists {
		state.LastAccessTime = time.Now()
		as.logger.Debug("recorded activity for function", zap.String("function", name))
	}
}

// RecordActivityBatch records invocations for multiple functions in a single lock acquisition.
// This is more efficient than calling RecordActivity multiple times for batch heartbeats.
func (as *AutoScaler) RecordActivityBatch(names []string) {
	if !as.config.Enabled || len(names) == 0 {
		return
	}

	as.functionsMutex.Lock()
	defer as.functionsMutex.Unlock()

	now := time.Now()
	for _, name := range names {
		if state, exists := as.functions[name]; exists {
			state.LastAccessTime = now
		}
	}
	as.logger.Debug("recorded batch activity", zap.Int("count", len(names)))
}

// IsScaledDown checks if a function is currently scaled down
func (as *AutoScaler) IsScaledDown(name string) bool {
	if !as.config.Enabled {
		return false
	}

	as.functionsMutex.RLock()
	defer as.functionsMutex.RUnlock()

	if state, exists := as.functions[name]; exists {
		return state.IsScaledDown
	}
	return false
}

// IsScalingDown checks if a function is currently in the process of scaling down
func (as *AutoScaler) IsScalingDown(name string) bool {
	if !as.config.Enabled {
		return false
	}

	as.functionsMutex.RLock()
	defer as.functionsMutex.RUnlock()

	if state, exists := as.functions[name]; exists {
		return state.IsScalingDown
	}
	return false
}

// MarkScaledDown marks a function as scaled down
func (as *AutoScaler) MarkScaledDown(name string, scaledDown bool) {
	if !as.config.Enabled {
		return
	}

	as.functionsMutex.Lock()
	defer as.functionsMutex.Unlock()

	if state, exists := as.functions[name]; exists {
		state.IsScaledDown = scaledDown
		as.logger.Debug("marked function scaled down", zap.String("function", name), zap.Bool("scaled_down", scaledDown))
	}
}

// MarkScalingDown marks a function as in the process of scaling down
func (as *AutoScaler) MarkScalingDown(name string, scalingDown bool) {
	if !as.config.Enabled {
		return
	}

	as.functionsMutex.Lock()
	defer as.functionsMutex.Unlock()

	if state, exists := as.functions[name]; exists {
		state.IsScalingDown = scalingDown
		as.logger.Debug("marked function scaling down", zap.String("function", name), zap.Bool("scaling_down", scalingDown))
	}
}

// GetFunctionStatus returns the current status of all functions
func (as *AutoScaler) GetFunctionStatus() map[string]FunctionState {
	as.functionsMutex.RLock()
	defer as.functionsMutex.RUnlock()

	status := make(map[string]FunctionState)
	for name, state := range as.functions {
		status[name] = *state
	}
	return status
}

// startMonitor periodically checks for idle functions and scales them down
func (as *AutoScaler) startMonitor() {
	as.logger.Info("autoscaler monitor loop started")
	ticker := time.NewTicker(as.config.CheckInterval)
	defer ticker.Stop()
	defer close(as.doneChan)

	for {
		select {
		case <-ticker.C:
			as.checkIdleFunctions()
		case <-as.stopChan:
			return
		}
	}
}

// checkIdleFunctions checks all functions and scales down idle ones
func (as *AutoScaler) checkIdleFunctions() {
	// Collect functions to scale down while holding the lock
	as.functionsMutex.Lock()
	now := time.Now()
	toScaleDown := []string{}

	for name, state := range as.functions {
		// Skip if scale-to-zero is disabled or already scaled down
		if !state.ScaleToZeroEnabled || state.IsScaledDown {
			continue
		}
		as.logger.Debug("checking function idle time", zap.String("function", name))
		idleTime := now.Sub(state.LastAccessTime)
		if idleTime > state.IdleDuration {
			as.logger.Info("function idle, scaling down",
				zap.String("function", name),
				zap.Duration("idle_time", idleTime),
				zap.Duration("threshold", state.IdleDuration))
			toScaleDown = append(toScaleDown, name)
		}
	}
	as.functionsMutex.Unlock()

	// Perform scale operations without holding the lock
	for _, name := range toScaleDown {
		err := as.scaleOperation.ScaleDown(name)

		// Update state after operation
		as.functionsMutex.Lock()
		if state, exists := as.functions[name]; exists {
			if err != nil {
				as.logger.Error("error scaling down function", zap.String("function", name), zap.Error(err))
			} else {
				state.IsScaledDown = true
				as.logger.Info("successfully scaled down function", zap.String("function", name))
			}
		}
		as.functionsMutex.Unlock()
	}
}

// parseScaleConfig parses scale-to-zero configuration from labels
// Supports both tinyFaaS and faasd label conventions
func (as *AutoScaler) parseScaleConfig(labels map[string]string, defaultDuration time.Duration) (bool, time.Duration) {
	// Check both label formats
	scaleZeroLabel := ""
	idleDurationLabel := ""

	switch as.config.Platform {
	case "faasd":
		// Check for com.openfaas.scale.zero (faasd)
		if val, ok := labels["com.openfaas.scale.zero"]; ok {
			scaleZeroLabel = val
		}
		if val, ok := labels["com.openfaas.scale.zero.idle_duration"]; ok {
			idleDurationLabel = val
		}
	case "tinyfaas":
		// Check for com.tinyfaas.scale.zero (tinyFaaS)
		if val, ok := labels["com.tinyfaas.scale.zero"]; ok {
			scaleZeroLabel = val
		}
		if val, ok := labels["com.tinyfaas.scale.zero.idle_duration"]; ok {
			idleDurationLabel = val
		}
	}

	// Parse scale-to-zero enabled flag
	scaleToZeroEnabled := as.config.Enabled // Default to enabled if autoscaler is enabled
	if scaleZeroLabel != "" {
		scaleToZeroEnabled = scaleZeroLabel == "true" || scaleZeroLabel == "1"
	}

	// Parse idle duration
	idleDuration := defaultDuration
	if idleDurationLabel != "" {
		if parsed, err := time.ParseDuration(idleDurationLabel); err == nil {
			idleDuration = parsed
		} else {
			as.logger.Warn("invalid idle_duration label, using default",
				zap.String("label_value", idleDurationLabel),
				zap.Duration("default_duration", defaultDuration),
				zap.Error(err))
		}
	}

	return scaleToZeroEnabled, idleDuration
}

// IsEnabled returns whether the autoscaler is enabled
func (as *AutoScaler) IsEnabled() bool {
	return as.config.Enabled
}

// GetIdleTime returns the idle time for a function
func (as *AutoScaler) GetIdleTime(name string) time.Duration {
	as.functionsMutex.RLock()
	defer as.functionsMutex.RUnlock()

	if state, exists := as.functions[name]; exists {
		return time.Since(state.LastAccessTime)
	}
	return 0
}
