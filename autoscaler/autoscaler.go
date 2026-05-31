package autoscaler

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"
)

// ScaleOperation defines the interface for scaling operations.
type ScaleOperation interface {
	// ScaleDown stops the function containers.
	ScaleDown(functionName string) error
	// ScaleUp starts the function containers.
	ScaleUp(functionName string) error
}

type LifecycleState string

const (
	StateActive      LifecycleState = "active"
	StateBlocked     LifecycleState = "blocked"
	StateScaledDown  LifecycleState = "scaled-down"
	StateScalingUp   LifecycleState = "scaling-up"
	StateScalingDown LifecycleState = "scaling-down"
)

// AutoScaler manages automatic scaling of functions based on activity.
type AutoScaler struct {
	config *Config

	functions      map[string]*functionEntry
	functionsMutex sync.RWMutex

	scaleOperation ScaleOperation

	stopChan chan struct{}
	doneChan chan struct{}
	logger   *slog.Logger
}

type FunctionState struct {
	Name               string
	LastAccessTime     time.Time
	LastActiveTime     time.Time
	IdleDuration       time.Duration
	ScaleToZeroEnabled bool
	State              LifecycleState
	InFlight           int
	Labels             map[string]string
}

type functionEntry struct {
	state FunctionState
	mu    sync.Mutex
	cond  *sync.Cond
}

// New creates a new AutoScaler instance.
func New(config Config, scaleOp ScaleOperation, logger *slog.Logger) *AutoScaler {
	if config.CheckInterval == 0 {
		config.CheckInterval = DEFAULT_CHECK_INTERVAL_SECONDS * time.Second
	}

	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	return &AutoScaler{
		config:         &config,
		functions:      make(map[string]*functionEntry),
		scaleOperation: scaleOp,
		stopChan:       make(chan struct{}),
		doneChan:       make(chan struct{}),
		logger:         logger,
	}
}

// Start begins the autoscaler monitoring loop.
func (as *AutoScaler) Start() {
	if !as.config.Enabled {
		as.logger.Info("autoscaler disabled, not starting monitor")
		return
	}

	as.logger.Info("starting monitor loop")
	go as.startMonitor()
}

// Stop gracefully shuts down the autoscaler.
func (as *AutoScaler) Stop() {
	if !as.config.Enabled {
		return
	}

	as.logger.Info("stopping monitor loop")
	close(as.stopChan)
	<-as.doneChan
	as.logger.Info("monitor loop stopped")
}

// RegisterFunction registers a function with initial active state.
func (as *AutoScaler) RegisterFunction(name string, labels map[string]string) {
	as.RegisterFunctionWithState(name, labels, StateActive)
}

// RegisterFunctionWithState registers a function with an explicit initial state.
func (as *AutoScaler) RegisterFunctionWithState(name string, labels map[string]string, initial LifecycleState) {
	if !as.config.Enabled {
		return
	}

	// As registering a function is typically done during deployment
	// The initial state should be either active or scaled-down.
	if initial != StateActive && initial != StateScaledDown {
		initial = StateActive
	}

	as.functionsMutex.Lock()
	defer as.functionsMutex.Unlock()

	scaleToZeroEnabled, idleDuration := as.parseScaleConfig(labels, as.config.DefaultIdleDuration)
	now := time.Now()

	labelCopy := map[string]string{}
	for k, v := range labels {
		labelCopy[k] = v
	}

	entry := &functionEntry{}
	entry.state = FunctionState{
		Name:               name,
		LastAccessTime:     now,
		LastActiveTime:     now,
		IdleDuration:       idleDuration,
		ScaleToZeroEnabled: scaleToZeroEnabled,
		State:              initial,
		InFlight:           0,
		Labels:             labelCopy,
	}
	entry.cond = sync.NewCond(&entry.mu)

	as.functions[name] = entry
	as.logger.Info("registered function", "function", name)
}

// UnregisterFunction removes a function from the autoscaler.
func (as *AutoScaler) UnregisterFunction(name string) {
	if !as.config.Enabled {
		return
	}

	as.functionsMutex.Lock()
	entry, ok := as.functions[name]
	if ok {
		delete(as.functions, name)
	}
	as.functionsMutex.Unlock()

	if ok {
		entry.mu.Lock()
		entry.cond.Broadcast()
		entry.mu.Unlock()
	}

	as.logger.Info("unregistered function", "function", name)
}

func (as *AutoScaler) getFunctionEntry(name string) (*functionEntry, bool) {
	as.functionsMutex.RLock()
	entry, ok := as.functions[name]
	as.functionsMutex.RUnlock()
	return entry, ok
}

// BeginInvocation marks a function invocation start and returns a completion callback.
func (as *AutoScaler) BeginInvocation(name string) (func(), error) {
	if !as.config.Enabled {
		return func() {}, nil
	}
	if err := as.StartInvocation(name); err != nil {
		return nil, err
	}

	called := false
	return func() {
		if called {
			return
		}
		called = true
		as.EndInvocation(name)
	}, nil
}

// StartInvocation marks that a function started handling a request.
func (as *AutoScaler) StartInvocation(name string) error {
	if !as.config.Enabled {
		return nil
	}

	entry, ok := as.getFunctionEntry(name)
	if !ok {
		return fmt.Errorf("function %s not registered", name)
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()

	for {
		switch entry.state.State {
		case StateScalingDown, StateScalingUp:
			entry.cond.Wait()
		case StateScaledDown:
			return fmt.Errorf("function %s is scaled down", name)
		case StateActive, StateBlocked:
			entry.state.InFlight++
			entry.state.LastAccessTime = time.Now()
			if entry.state.State == StateActive {
				entry.state.State = StateBlocked
			}
			entry.cond.Broadcast()
			return nil
		default:
			return fmt.Errorf("function %s has invalid state %s", name, entry.state.State)
		}
	}
}

// EndInvocation marks that a function finished handling a request.
func (as *AutoScaler) EndInvocation(name string) {
	if !as.config.Enabled {
		return
	}

	entry, ok := as.getFunctionEntry(name)
	if !ok {
		return
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.state.InFlight > 0 {
		entry.state.InFlight--
	}
	now := time.Now()
	entry.state.LastAccessTime = now
	if entry.state.InFlight == 0 && entry.state.State == StateBlocked {
		entry.state.State = StateActive
		entry.state.LastActiveTime = now
	}
	entry.cond.Broadcast()
}

// ScaleUpWhenReady ensures function runtime is available.
// StateBlocked is treated as ready because runtime is present and handling in-flight requests
func (as *AutoScaler) ScaleUpWhenReady(ctx context.Context, name string) error {
	if !as.config.Enabled {
		return nil
	}

	entry, ok := as.getFunctionEntry(name)
	if !ok {
		return fmt.Errorf("function %s not registered", name)
	}

	for {
		entry.mu.Lock()
		state := entry.state.State
		switch state {
		case StateActive, StateBlocked:
			now := time.Now()
			entry.state.LastAccessTime = now
			if state == StateActive {
				entry.state.LastActiveTime = now
			}
			entry.cond.Broadcast()
			entry.mu.Unlock()
			return nil
		case StateScalingUp, StateScalingDown:
			if err := waitWithContext(ctx, entry.cond, &entry.mu); err != nil {
				entry.mu.Unlock()
				return err
			}
			entry.mu.Unlock()
			continue
		case StateScaledDown:
			entry.state.State = StateScalingUp
			entry.cond.Broadcast()
			entry.mu.Unlock()

			err := as.scaleOperation.ScaleUp(name)

			entry.mu.Lock()
			if err != nil {
				entry.state.State = StateScaledDown
				entry.cond.Broadcast()
				entry.mu.Unlock()
				return err
			}
			now := time.Now()
			entry.state.State = StateActive
			entry.state.LastAccessTime = now
			entry.state.LastActiveTime = now
			entry.cond.Broadcast()
			entry.mu.Unlock()
			return nil
		default:
			entry.mu.Unlock()
			return fmt.Errorf("function %s has invalid state %s", name, state)
		}
	}
}

// ScaleDownWhenIdle performs a safe blocking scale-down transition once no request is in flight.
func (as *AutoScaler) ScaleDownWhenIdle(ctx context.Context, name string) error {
	if !as.config.Enabled {
		return nil
	}

	entry, ok := as.getFunctionEntry(name)
	if !ok {
		return fmt.Errorf("function %s not registered", name)
	}

	for {
		entry.mu.Lock()
		state := entry.state.State
		switch state {
		case StateScaledDown:
			entry.mu.Unlock()
			return nil
		case StateBlocked, StateScalingUp, StateScalingDown:
			if err := waitWithContext(ctx, entry.cond, &entry.mu); err != nil {
				entry.mu.Unlock()
				return err
			}
			entry.mu.Unlock()
			continue
		case StateActive:
			now := time.Now()
			if entry.state.ScaleToZeroEnabled && now.Sub(entry.state.LastActiveTime) <= entry.state.IdleDuration {
				entry.state.LastAccessTime = now
				entry.mu.Unlock()
				return nil
			}
			entry.state.State = StateScalingDown
			entry.cond.Broadcast()
			entry.mu.Unlock()

			err := as.scaleOperation.ScaleDown(name)

			entry.mu.Lock()
			if err != nil {
				entry.state.State = StateActive
				entry.state.LastActiveTime = time.Now()
				entry.cond.Broadcast()
				entry.mu.Unlock()
				return err
			}
			entry.state.State = StateScaledDown
			entry.state.InFlight = 0
			entry.state.LastAccessTime = time.Now()
			entry.cond.Broadcast()
			entry.mu.Unlock()
			return nil
		default:
			entry.mu.Unlock()
			return fmt.Errorf("function %s has invalid state %s", name, state)
		}
	}
}

func waitWithContext(ctx context.Context, cond *sync.Cond, mu *sync.Mutex) error {
	if ctx == nil {
		cond.Wait()
		return nil
	}

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			mu.Lock()
			cond.Broadcast()
			mu.Unlock()
		case <-done:
		}
	}()
	cond.Wait()
	close(done)
	return ctx.Err()
}

func (as *AutoScaler) ScaleDown(functionName string) error {
	return as.ScaleDownWhenIdle(context.Background(), functionName)
}

func (as *AutoScaler) ScaleUp(functionName string) error {
	return as.ScaleUpWhenReady(context.Background(), functionName)
}

// RecordActivity keeps compatibility and refreshes activity timestamps.
func (as *AutoScaler) RecordActivity(name string) {
	if !as.config.Enabled {
		return
	}

	entry, ok := as.getFunctionEntry(name)
	if !ok {
		return
	}

	entry.mu.Lock()
	now := time.Now()
	entry.state.LastAccessTime = now
	if entry.state.State == StateActive {
		entry.state.LastActiveTime = now
	}
	entry.mu.Unlock()
}

// RecordActivityBatch records invocations for multiple functions in one pass.
func (as *AutoScaler) RecordActivityBatch(names []string) {
	if !as.config.Enabled || len(names) == 0 {
		return
	}

	now := time.Now()
	for _, name := range names {
		entry, ok := as.getFunctionEntry(name)
		if !ok {
			continue
		}
		entry.mu.Lock()
		entry.state.LastAccessTime = now
		if entry.state.State == StateActive {
			entry.state.LastActiveTime = now
		}
		entry.mu.Unlock()
	}
}

// IsScaledDown checks if a function is currently scaled down.
func (as *AutoScaler) IsScaledDown(name string) bool {
	if !as.config.Enabled {
		return false
	}

	entry, ok := as.getFunctionEntry(name)
	if !ok {
		return false
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	return entry.state.State == StateScaledDown
}

// IsScalingDown checks if a function is currently in the process of scaling down.
func (as *AutoScaler) IsScalingDown(name string) bool {
	if !as.config.Enabled {
		return false
	}

	entry, ok := as.getFunctionEntry(name)
	if !ok {
		return false
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	return entry.state.State == StateScalingDown
}

// GetState returns a function's current lifecycle state.
func (as *AutoScaler) GetState(name string) (LifecycleState, bool) {
	entry, ok := as.getFunctionEntry(name)
	if !ok {
		return "", false
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	return entry.state.State, true
}

// GetFunctionStatus returns the current status of all functions.
func (as *AutoScaler) GetFunctionStatus() map[string]FunctionState {
	as.functionsMutex.RLock()
	names := make([]string, 0, len(as.functions))
	entries := make(map[string]*functionEntry, len(as.functions))
	for name, entry := range as.functions {
		names = append(names, name)
		entries[name] = entry
	}
	as.functionsMutex.RUnlock()

	status := make(map[string]FunctionState, len(entries))
	for _, name := range names {
		entry := entries[name]
		entry.mu.Lock()
		copyState := entry.state
		copyLabels := map[string]string{}
		for k, v := range copyState.Labels {
			copyLabels[k] = v
		}
		copyState.Labels = copyLabels
		entry.mu.Unlock()
		status[name] = copyState
	}
	return status
}

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

func (as *AutoScaler) checkIdleFunctions() {
	if !as.config.Enabled {
		return
	}

	as.functionsMutex.RLock()
	entries := make(map[string]*functionEntry, len(as.functions))
	for name, entry := range as.functions {
		entries[name] = entry
	}
	as.functionsMutex.RUnlock()

	now := time.Now()
	for name, entry := range entries {
		entry.mu.Lock()
		if !entry.state.ScaleToZeroEnabled {
			entry.mu.Unlock()
			continue
		}
		if entry.state.State != StateActive {
			entry.mu.Unlock()
			continue
		}

		idleTime := now.Sub(entry.state.LastActiveTime)
		idleDuration := entry.state.IdleDuration
		entry.mu.Unlock()

		if idleTime > idleDuration {
			as.logger.Info("function idle, scaling down", "function", name)
			if err := as.ScaleDownWhenIdle(context.Background(), name); err != nil {
				as.logger.Error("error scaling down function", "function", name, "err", err)
			} else {
				as.logger.Info("function scaled down", "function", name)
			}
		}
	}
}

func (as *AutoScaler) parseScaleConfig(labels map[string]string, defaultDuration time.Duration) (bool, time.Duration) {
	scaleZeroLabel := ""
	idleDurationLabel := ""

	switch as.config.Platform {
	case "faasd":
		if val, ok := labels["com.openfaas.scale.zero"]; ok {
			scaleZeroLabel = val
		}
		if val, ok := labels["com.openfaas.scale.zero.idle_duration"]; ok {
			idleDurationLabel = val
		}
	case "tinyfaas":
		if val, ok := labels["com.tinyfaas.scale.zero"]; ok {
			scaleZeroLabel = val
		}
		if val, ok := labels["com.tinyfaas.scale.zero.idle_duration"]; ok {
			idleDurationLabel = val
		}
	}

	scaleToZeroEnabled := as.config.Enabled
	if scaleZeroLabel != "" {
		scaleToZeroEnabled = scaleZeroLabel == "true" || scaleZeroLabel == "1"
	}

	idleDuration := defaultDuration
	if idleDurationLabel != "" {
		if parsed, err := time.ParseDuration(idleDurationLabel); err == nil {
			idleDuration = parsed
		} else {
			as.logger.Error("invalid idle_duration label, using default", "label", idleDurationLabel, "err", err)
		}
	}

	return scaleToZeroEnabled, idleDuration
}

// IsEnabled returns whether the autoscaler is enabled.
func (as *AutoScaler) IsEnabled() bool {
	return as.config.Enabled
}

// GetIdleTime returns idle time for a function while active; otherwise 0.
func (as *AutoScaler) GetIdleTime(name string) time.Duration {
	entry, ok := as.getFunctionEntry(name)
	if !ok {
		return 0
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.state.State != StateActive {
		return 0
	}
	return time.Since(entry.state.LastActiveTime)
}
