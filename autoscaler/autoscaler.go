package autoscaler

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ScaleOperation defines the interface for scaling operations.
type ScaleOperation interface {
	// ScaleDown stops the function containers.
	ScaleDown(functionName string) error
	// ScaleUp starts the function containers.
	ScaleUp(functionName string) error
}

// LifecycleState represents the lifecycle state of a registered function.
type LifecycleState string

const (
	StateActive      LifecycleState = "active"
	StateBlocked     LifecycleState = "blocked"
	StateScaledDown  LifecycleState = "scaled-down"
	StateScalingUp   LifecycleState = "scaling-up"
	StateScalingDown LifecycleState = "scaling-down"
)

var (
	// ErrFunctionNotFound is returned when an operation targets a function that is not registered.
	ErrFunctionNotFound = errors.New("autoscaler: function not registered")
	// ErrFunctionUnregistered is returned to operations that were waiting on a function
	// when it was unregistered (or replaced via a re-register of the same name).
	ErrFunctionUnregistered = errors.New("autoscaler: function unregistered while waiting")
	// ErrFunctionScaledDown is returned by StartInvocation when the function is currently scaled down.
	ErrFunctionScaledDown = errors.New("autoscaler: function is scaled down")
	// ErrAutoscalerStopped is returned to waiters that are unblocked because Stop() was called.
	ErrAutoscalerStopped = errors.New("autoscaler: stopped")
	// ErrInvalidState is returned when an operation encounters an unexpected lifecycle state.
	ErrInvalidState = errors.New("autoscaler: invalid lifecycle state")
)

// AutoScaler manages automatic scaling of functions based on activity.
type AutoScaler struct {
	config *Config

	functions      map[string]*functionEntry
	functionsMutex sync.RWMutex

	scaleOperation ScaleOperation

	// Lifecycle: started/stopOnce protect Start() and Stop() from being
	// invoked multiple times concurrently.
	started  atomic.Bool
	stopOnce sync.Once
	stopChan chan struct{} // closed by Stop()
	doneChan chan struct{} // closed when monitor exits, or by Stop() if monitor never ran

	// scaleDownWG tracks workers spawned by the monitor's idle scan so that
	// Stop() can wait for them to actually exit (cancellation propagates
	// via stopChan -> per-call ctx, but we still need to join them).
	scaleDownWG sync.WaitGroup

	// tickInProgress prevents overlapping idle scans: if a tick's worker
	// pool is still draining when the next tick fires, the new tick is
	// skipped. The flag is reset by checkIdleFunctions just before
	// returning.
	tickInProgress atomic.Bool

	logger *slog.Logger
}

// FunctionState represents the externally visible state for a single function.
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

// functionEntry is the per-function bookkeeping. State mutations and channel
// rotations always happen under mu.
type functionEntry struct {
	mu    sync.Mutex
	state FunctionState

	// changed is closed and replaced every time the state mutates. Waiters
	// capture the current channel under mu, drop mu, then select on it.
	// Replacing the channel after close means a future close represents a
	// future event - no spurious wakeups across rotations.
	changed chan struct{}

	// unregistered is closed exactly once when this entry is removed from
	// the AutoScaler. It is never replaced - waiters that captured it can
	// rely on it staying closed forever.
	unregistered chan struct{}
}

// notifyLocked publishes a state change to any goroutine waiting on the entry.
// Caller must hold e.mu.
func (e *functionEntry) notifyLocked() {
	close(e.changed)
	e.changed = make(chan struct{})
}

// closeUnregistered marks the entry as removed. Idempotent. Caller must NOT
// hold e.mu (this only manipulates the unregistered channel, which is one-shot
// and does not require the mutex).
func (e *functionEntry) closeUnregistered() {
	// Use a select on a closed channel pattern to make close() idempotent
	// without an extra atomic flag.
	select {
	case <-e.unregistered:
		// already closed
	default:
		close(e.unregistered)
	}
}

// waitForChange releases mu, blocks until the next state change, the entry's
// unregistration, autoscaler shutdown, or ctx cancellation. It re-acquires mu
// before returning so callers can keep using the standard for/switch pattern
// without juggling lock state across return paths.
//
// Returns:
//
//	nil                       -> state changed, caller should re-evaluate.
//	ErrFunctionUnregistered  -> entry was removed.
//	ErrAutoscalerStopped     -> AutoScaler.Stop() was called.
func (e *functionEntry) waitForChange(stopCh <-chan struct{}) error {
	ch := e.changed
	unreg := e.unregistered
	e.mu.Unlock()
	defer e.mu.Lock()

	select {
	case <-ch:
		return nil
	case <-unreg:
		return ErrFunctionUnregistered
	case <-stopCh:
		return ErrAutoscalerStopped
	}
}

// New creates a new AutoScaler instance.
//
// scaleOp may be nil only when config.Enabled is false; otherwise New panics.
// config.Platform is normalized to lowercase so callers can supply either case.
func New(config Config, scaleOp ScaleOperation, logger *slog.Logger) *AutoScaler {
	if config.CheckInterval == 0 {
		config.CheckInterval = DEFAULT_CHECK_INTERVAL_SECONDS * time.Second
	}
	if config.MaxConcurrentScaleDowns <= 0 {
		config.MaxConcurrentScaleDowns = DEFAULT_MAX_CONCURRENT_SCALE_DOWNS
	}

	// Normalize platform once so parseScaleConfig's switch always matches,
	// regardless of how callers built the Config struct.
	config.Platform = strings.ToLower(config.Platform)

	if config.Enabled && scaleOp == nil {
		panic("autoscaler: New called with nil ScaleOperation while Enabled=true")
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

// Start begins the autoscaler monitoring loop. Idempotent and safe to call
// from multiple goroutines.
func (as *AutoScaler) Start() {
	if !as.config.Enabled {
		as.logger.Info("autoscaler disabled, not starting monitor")
		return
	}
	if !as.started.CompareAndSwap(false, true) {
		// already started
		return
	}

	as.logger.Info("starting monitor loop")
	go as.startMonitor()
}

// Stop gracefully shuts down the autoscaler. Idempotent and safe to call
// without a preceding Start(). Any goroutines blocked inside StartInvocation,
// ScaleUpWhenReady, or ScaleDownWhenIdle will return ErrAutoscalerStopped.
//
// Stop waits for the monitor goroutine to exit and then for any workers it
// spawned (concurrent scale-down operations) to also return. Cancellation
// propagates via stopChan into each worker, so well-behaved hosts return
// promptly.
func (as *AutoScaler) Stop() {
	if !as.config.Enabled {
		return
	}

	as.stopOnce.Do(func() {
		as.logger.Info("stopping autoscaler")
		close(as.stopChan)
		// If the monitor goroutine never ran (Start was not called), close
		// doneChan eagerly so the wait below does not deadlock.
		if !as.started.Load() {
			close(as.doneChan)
		}
	})

	<-as.doneChan
	// Workers spawned by the most recent idle scan may still be returning.
	// Wait for them so the host runtime sees a clean shutdown sequence.
	as.scaleDownWG.Wait()
	as.logger.Info("autoscaler stopped")
}

// RegisterFunction registers a function with initial active state.
func (as *AutoScaler) RegisterFunction(name string, labels map[string]string) {
	as.RegisterFunctionWithState(name, labels, StateActive)
}

// RegisterFunctionWithState registers a function with an explicit initial state.
// If a function with the same name was previously registered, its waiters are
// released with ErrFunctionUnregistered before the new entry is installed.
func (as *AutoScaler) RegisterFunctionWithState(name string, labels map[string]string, initial LifecycleState) {
	if !as.config.Enabled {
		return
	}

	// As registering a function is typically done during deployment, the
	// initial state should be either active or scaled-down.
	if initial != StateActive && initial != StateScaledDown {
		initial = StateActive
	}

	scaleToZeroEnabled, idleDuration := as.parseScaleConfig(labels, as.config.DefaultIdleDuration)
	now := time.Now()

	labelCopy := map[string]string{}
	for k, v := range labels {
		labelCopy[k] = v
	}

	entry := &functionEntry{
		changed:      make(chan struct{}),
		unregistered: make(chan struct{}),
		state: FunctionState{
			Name:               name,
			LastAccessTime:     now,
			LastActiveTime:     now,
			IdleDuration:       idleDuration,
			ScaleToZeroEnabled: scaleToZeroEnabled,
			State:              initial,
			InFlight:           0,
			Labels:             labelCopy,
		},
	}

	as.functionsMutex.Lock()
	old, hadOld := as.functions[name]
	as.functions[name] = entry
	as.functionsMutex.Unlock()

	if hadOld {
		// Release waiters on the old entry so they don't observe a stale
		// state machine. They'll see ErrFunctionUnregistered.
		old.closeUnregistered()
	}

	as.logger.Info("registered function", "function", name)
}

// UnregisterFunction removes a function from the autoscaler. Goroutines
// currently blocked on the function's lifecycle (StartInvocation,
// ScaleUpWhenReady, ScaleDownWhenIdle) will return with ErrFunctionUnregistered.
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
		entry.closeUnregistered()
	}

	as.logger.Info("unregistered function", "function", name)
}

func (as *AutoScaler) getFunctionEntry(name string) (*functionEntry, bool) {
	as.functionsMutex.RLock()
	entry, ok := as.functions[name]
	as.functionsMutex.RUnlock()
	return entry, ok
}

// BeginInvocation marks a function invocation start and returns a completion
// callback. The callback may be invoked from any goroutine and is idempotent
// (only the first call has an effect).
func (as *AutoScaler) BeginInvocation(name string) (func(), error) {
	if !as.config.Enabled {
		return func() {}, nil
	}
	if err := as.StartInvocation(name); err != nil {
		return nil, err
	}

	var done atomic.Bool
	return func() {
		if done.CompareAndSwap(false, true) {
			as.EndInvocation(name)
		}
	}, nil
}

// StartInvocation marks that a function started handling a request. It blocks
// while the function is in a transient state (ScalingUp/ScalingDown).
func (as *AutoScaler) StartInvocation(name string) error {
	if !as.config.Enabled {
		return nil
	}

	entry, ok := as.getFunctionEntry(name)
	if !ok {
		return fmt.Errorf("%w: %s", ErrFunctionNotFound, name)
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()

	for {
		switch entry.state.State {
		case StateScalingDown, StateScalingUp:
			if err := entry.waitForChange(as.stopChan); err != nil {
				return err
			}
			// re-evaluate
		case StateScaledDown:
			return fmt.Errorf("%w: %s", ErrFunctionScaledDown, name)
		case StateActive, StateBlocked:
			entry.state.InFlight++
			entry.state.LastAccessTime = time.Now()
			if entry.state.State == StateActive {
				entry.state.State = StateBlocked
			}
			entry.notifyLocked()
			return nil
		default:
			return fmt.Errorf("%w: %s state=%s", ErrInvalidState, name, entry.state.State)
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
	entry.notifyLocked()
}

// ScaleUpWhenReady ensures function runtime is available. StateBlocked is
// treated as ready because runtime is present and handling in-flight requests.
func (as *AutoScaler) ScaleUpWhenReady(name string) error {
	if !as.config.Enabled {
		return nil
	}

	entry, ok := as.getFunctionEntry(name)
	if !ok {
		return fmt.Errorf("%w: %s", ErrFunctionNotFound, name)
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
			// No state transition, but record the access. We do not need
			// to notify anyone - timestamp updates are not waited on.
			entry.mu.Unlock()
			return nil
		case StateScalingUp, StateScalingDown:
			if err := entry.waitForChange(as.stopChan); err != nil {
				entry.mu.Unlock()
				return err
			}
			entry.mu.Unlock()
			continue
		case StateScaledDown:
			entry.state.State = StateScalingUp
			entry.notifyLocked()
			entry.mu.Unlock()

			err := as.scaleOperation.ScaleUp(name)

			entry.mu.Lock()
			if err != nil {
				entry.state.State = StateScaledDown
				entry.notifyLocked()
				entry.mu.Unlock()
				return err
			}
			now := time.Now()
			entry.state.State = StateActive
			entry.state.LastAccessTime = now
			entry.state.LastActiveTime = now
			entry.notifyLocked()
			entry.mu.Unlock()
			return nil
		default:
			entry.mu.Unlock()
			return fmt.Errorf("%w: %s state=%s", ErrInvalidState, name, state)
		}
	}
}

// ScaleDownWhenIdle performs a blocking scale-down transition once no request
// is in flight. The function is scaled down unconditionally - callers are
// expected to be the authority on the decision (e.g. a redeploy, an explicit
// replicas=0 request, or the monitor's idle check). The monitor performs its
// own freshness check before calling this method; consequently we do NOT
// short-circuit on a recent activity timestamp here, since that would silently
// drop forced scale-down requests issued by other code paths.
func (as *AutoScaler) ScaleDownWhenIdle(name string) error {
	if !as.config.Enabled {
		return nil
	}

	entry, ok := as.getFunctionEntry(name)
	if !ok {
		return fmt.Errorf("%w: %s", ErrFunctionNotFound, name)
	}

	for {
		entry.mu.Lock()
		state := entry.state.State
		switch state {
		case StateScaledDown:
			entry.mu.Unlock()
			return nil
		case StateBlocked, StateScalingUp, StateScalingDown:
			if err := entry.waitForChange(as.stopChan); err != nil {
				entry.mu.Unlock()
				return err
			}
			entry.mu.Unlock()
			continue
		case StateActive:
			entry.state.State = StateScalingDown
			entry.notifyLocked()
			entry.mu.Unlock()

			err := as.scaleOperation.ScaleDown(name)

			entry.mu.Lock()
			if err != nil {
				entry.state.State = StateActive
				entry.state.LastActiveTime = time.Now()
				entry.notifyLocked()
				entry.mu.Unlock()
				return err
			}
			entry.state.State = StateScaledDown
			// Defensive: by FSM invariant InFlight is already 0 here.
			entry.state.InFlight = 0
			entry.state.LastAccessTime = time.Now()
			entry.notifyLocked()
			entry.mu.Unlock()
			return nil
		default:
			entry.mu.Unlock()
			return fmt.Errorf("%w: %s state=%s", ErrInvalidState, name, state)
		}
	}
}

// RecordActivity refreshes activity timestamps for a function. No-op if the
// function is not registered.
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
	as.logger.Info("autoscaler monitor loop started", "check_interval", as.config.CheckInterval, "max_concurrent_scale_downs", as.config.MaxConcurrentScaleDowns)
	ticker := time.NewTicker(as.config.CheckInterval)
	defer ticker.Stop()
	defer close(as.doneChan)

	for {
		select {
		case <-ticker.C:
			// If the previous tick's worker pool is still draining, skip
			// this tick rather than stack up overlapping batches. The
			// next tick will see the latest state.
			if !as.tickInProgress.CompareAndSwap(false, true) {
				as.logger.Debug("previous idle scan still in progress, skipping tick")
				continue
			}
			as.checkIdleFunctions()
			as.tickInProgress.Store(false)
		case <-as.stopChan:
			return
		}
	}
}

// checkIdleFunctions runs the monitor's idle scan: snapshots the registered
// functions and dispatches scale-down attempts to a bounded worker pool. It
// returns only after every worker has exited, so the caller (the monitor
// goroutine) can safely use as.tickInProgress as a serialization gate.
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

	if len(entries) == 0 {
		return
	}

	type job struct {
		name  string
		entry *functionEntry
	}
	jobs := make(chan job, len(entries))
	for name, entry := range entries {
		jobs <- job{name, entry}
	}
	close(jobs)

	workers := as.config.MaxConcurrentScaleDowns
	if workers > len(entries) {
		workers = len(entries)
	}
	if workers < 1 {
		workers = 1
	}

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		// Track via the autoscaler-level WG too, so Stop() can wait
		// for outstanding work even if the monitor loop has exited.
		as.scaleDownWG.Add(1)
		go func() {
			defer wg.Done()
			defer as.scaleDownWG.Done()

			for j := range jobs {
				// Bail promptly on shutdown; abandon remaining jobs.
				select {
				case <-as.stopChan:
					return
				default:
				}

				scaledDown, err := as.scaleDownIfStillIdle(j.name, j.entry)

				switch {
				case err == nil:
					if scaledDown {
						as.logger.Info("function scaled down", "function", j.name)
					}
				case errors.Is(err, ErrAutoscalerStopped),
					errors.Is(err, ErrFunctionUnregistered):
					// Not actionable; will retry next tick (if applicable).
					as.logger.Warn("scale-down skipped", "function", j.name, "err", err)
				default:
					as.logger.Error("error scaling down function", "function", j.name, "err", err)
				}
			}
		}()
	}

	wg.Wait()
}

// scaleDownIfStillIdle is the monitor-side scale-down primitive. Unlike the
// public ScaleDownWhenIdle (which is unconditional and used by callers that
// have already decided to scale down), this method:
//
//  1. checks ScaleToZeroEnabled and the idle window under the entry lock,
//  2. SKIPS functions that are in any transient state (Blocked / ScalingUp /
//     ScalingDown). The monitor's job is opportunistic - the next tick will
//     re-evaluate. This avoids stalling the monitor goroutine on a single
//     function when many are registered,
//  3. only commits StateScalingDown when the function is Active AND still idle.
//
// The boolean return indicates whether a scale-down actually fired.
func (as *AutoScaler) scaleDownIfStillIdle(name string, entry *functionEntry) (bool, error) {
	entry.mu.Lock()

	if !entry.state.ScaleToZeroEnabled {
		entry.mu.Unlock()
		return false, nil
	}

	switch entry.state.State {
	case StateActive:
		// fall through
	case StateScaledDown, StateBlocked, StateScalingUp, StateScalingDown:
		// Not eligible for scale-down right now. Try again next tick.
		entry.mu.Unlock()
		return false, nil
	default:
		state := entry.state.State
		entry.mu.Unlock()
		return false, fmt.Errorf("%w: %s state=%s", ErrInvalidState, name, state)
	}

	now := time.Now()
	if now.Sub(entry.state.LastActiveTime) <= entry.state.IdleDuration {
		// Activity is fresh - leave it alone.
		entry.mu.Unlock()
		return false, nil
	}

	entry.state.State = StateScalingDown
	entry.notifyLocked()
	entry.mu.Unlock()

	// Bail before invoking the host if shutdown was requested between
	// dispatching this job and us picking it up.
	select {
	case <-as.stopChan:
		entry.mu.Lock()
		entry.state.State = StateActive
		entry.notifyLocked()
		entry.mu.Unlock()
		return false, ErrAutoscalerStopped
	default:
	}

	scaleErr := as.scaleOperation.ScaleDown(name)

	entry.mu.Lock()
	if scaleErr != nil {
		entry.state.State = StateActive
		entry.state.LastActiveTime = time.Now()
		entry.notifyLocked()
		entry.mu.Unlock()
		return false, scaleErr
	}
	entry.state.State = StateScaledDown
	entry.state.InFlight = 0
	entry.state.LastAccessTime = time.Now()
	entry.notifyLocked()
	entry.mu.Unlock()
	return true, nil
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
