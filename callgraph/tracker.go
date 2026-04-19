package callgraph

import (
	"encoding/json"
	"errors"
	"sort"
	"time"

	"go.uber.org/zap"
)

const (
	// DefaultContextTTL is the default time-to-live for execution contexts (5 minutes)
	DefaultContextTTL = 5 * time.Minute
	// DefaultContextCleanupInterval is the default interval for cleanup goroutine (1 minute)
	DefaultContextCleanupInterval = 1 * time.Minute
)

// New creates a new CallGraphTracker with default configuration
func New(options ...Option) *CallGraphTracker {
	tracker := &CallGraphTracker{
		config:            DefaultConfig(),
		logger:            DefaultLogger(),
		edgeStats:         make(map[string]*edgeStats),
		functionStats:     make(map[string]*FunctionStats),
		callerToCallees:   make(map[string]map[string]bool),
		calleeToCallers:   make(map[string]map[string]bool),
		executionContexts: make(map[string]map[string]executionContext),
		contextLastAccess: make(map[string]time.Time),
		startTime:         time.Now(),
		cleanupStop:       make(chan struct{}),
		cleanupDone:       make(chan struct{}),
	}
	applyAveragingConfig(tracker, tracker.config)
	for _, option := range options {
		option(tracker)
	}

	return tracker
}

func WithConfig(config *Config) Option {
	return func(tracker *CallGraphTracker) {
		if err := validateConfig(config); err != nil {
			panic(err)
		}
		tracker.config = config
		applyAveragingConfig(tracker, config)
	}
}

func WithSMA(smaConfig *SMAConfig) Option {
	return func(tracker *CallGraphTracker) {
		tracker.averagingMethod = SimpleMovingAverage
		if smaConfig == nil {
			smaConfig = &SMAConfig{WindowSize: DEFAULT_SMA_WINDOW_SIZE}
		}
		tracker.smaConfig = smaConfig
		tracker.emaConfig = nil
	}
}

func WithEMA(emaConfig *EMAConfig) Option {
	return func(tracker *CallGraphTracker) {
		tracker.averagingMethod = ExponentialMovingAverage
		if emaConfig == nil {
			emaConfig = &EMAConfig{Alpha: DEFAULT_EMA_ALPHA}
		}
		tracker.emaConfig = emaConfig
		tracker.smaConfig = nil
	}
}

func WithLogger(logger *zap.Logger) Option {
	return func(tracker *CallGraphTracker) {
		tracker.logger = logger
	}
}

func DefaultLogger() *zap.Logger {
	logger, _ := zap.NewProduction()
	return logger
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		Enabled:                true,
		Method:                 SimpleMovingAverage,
		SMAConfig:              &SMAConfig{WindowSize: DEFAULT_SMA_WINDOW_SIZE},
		ContextTTL:             DefaultContextTTL,
		ContextCleanupInterval: DefaultContextCleanupInterval,
		Prewarm:                DefaultPrewarmConfig(),
	}
}

func applyAveragingConfig(tracker *CallGraphTracker, config *Config) {
	if config == nil {
		WithSMA(&SMAConfig{WindowSize: DEFAULT_SMA_WINDOW_SIZE})(tracker)
		return
	}

	switch config.Method {
	case ExponentialMovingAverage:
		emaConfig := config.EMAConfig
		if emaConfig == nil {
			emaConfig = &EMAConfig{Alpha: DEFAULT_EMA_ALPHA}
		}
		WithEMA(emaConfig)(tracker)
	case SimpleMovingAverage:
		fallthrough
	default:
		smaConfig := config.SMAConfig
		if smaConfig == nil {
			smaConfig = &SMAConfig{WindowSize: DEFAULT_SMA_WINDOW_SIZE}
		}
		WithSMA(smaConfig)(tracker)
	}
}

func validateConfig(config *Config) error {
	if config.ContextTTL <= 0 {
		return errors.New("ContextTTL must be non-zero positive")
	}

	if config.ContextCleanupInterval <= 0 {
		return errors.New("ContextCleanupInterval cannot be negative")
	}
	return nil
}

func (t *CallGraphTracker) GetAverageMethodConfig() any {
	switch t.averagingMethod {
	case ExponentialMovingAverage:
		return t.emaConfig
	case SimpleMovingAverage:
		return t.smaConfig
	default:
		return nil
	}
}

// ====== Implementation for Tracker interface ======

// Enabled returns whether the call graph tracking is enabled
func (t *CallGraphTracker) Enabled() bool {
	return t.config.Enabled
}

// PrewarmEnabled returns whether prewarming is enabled
func (t *CallGraphTracker) PrewarmEnabled() bool {
	return t.config.Prewarm != nil && t.config.Prewarm.Enabled
}

// StartExecution marks the beginning of a function execution for a specific request
func (t *CallGraphTracker) StartExecution(functionName string, requestID string, executionID string, timestamp time.Time) {
	if !t.config.Enabled {
		return
	}

	if functionName == "" || requestID == "" || executionID == "" {
		t.logger.Warn("functionName, requestID, or executionID is empty",
			zap.String("function", functionName),
			zap.String("requestID", requestID),
			zap.String("executionID", executionID))
		return
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()

	// Initialize request context if needed
	if t.executionContexts[requestID] == nil {
		t.executionContexts[requestID] = make(map[string]executionContext)
	}

	// Store start time for this function in this request
	t.executionContexts[requestID][executionID] = executionContext{
		functionName: functionName,
		startTime:    timestamp,
	}

	// Update last access time for TTL tracking
	t.contextLastAccess[requestID] = time.Now()

	t.logger.Debug("started execution",
		zap.String("function", functionName),
		zap.String("requestID", requestID),
		zap.String("executionID", executionID),
		zap.Time("timestamp", timestamp))
}

// RecordEdge records an edge in the call graph when caller invokes callee
// It automatically calculates edge execution time from the execution context
// caller is empty string for external calls (entry points)
func (t *CallGraphTracker) RecordEdge(caller, callee string, requestID string, callerExecutionID string, timestamp time.Time) {
	if !t.config.Enabled {
		return
	}

	// Validate: callee cannot be empty
	if callee == "" {
		return
	}

	// Prevent self-loops: a function cannot call itself
	if caller != "" && caller == callee {
		t.logger.Warn("self-loop detected, skipping edge recording")
		return
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()

	var executionTime time.Duration

	// If this is an internal call (caller is not empty), calculate edge execution time
	if caller != "" && requestID != "" {
		// Look up when caller started in this request
		if requestCtx, ok := t.executionContexts[requestID]; ok {
			if callerExecutionID == "" {
				t.logger.Warn("caller executionID is empty",
					zap.String("caller", caller),
					zap.String("callee", callee),
					zap.String("requestID", requestID))
				// Can't calculate edge time, skip recording
				return
			}
			if ctx, ok := requestCtx[callerExecutionID]; ok {
				if ctx.functionName != caller {
					t.logger.Warn("caller executionID does not match caller",
						zap.String("caller", caller),
						zap.String("callee", callee),
						zap.String("requestID", requestID),
						zap.String("executionID", callerExecutionID))
					// Can't calculate edge time, skip recording
					return
				}
				executionTime = timestamp.Sub(ctx.startTime)
				t.logger.Debug("calculated edge execution time",
					zap.String("caller", caller),
					zap.String("callee", callee),
					zap.String("requestID", requestID),
					zap.String("executionID", callerExecutionID),
					zap.Duration("executionTime", executionTime))
			} else {
				t.logger.Warn("caller execution context not found",
					zap.String("caller", caller),
					zap.String("callee", callee),
					zap.String("requestID", requestID),
					zap.String("executionID", callerExecutionID))
				// Can't calculate edge time, skip recording
				return
			}
		} else {
			// Can't calculate edge time, skip recording
			t.logger.Warn("request context not found",
				zap.String("requestID", requestID),
				zap.String("caller", caller),
				zap.String("callee", callee))
			return
		}
	}

	// Update edge statistics
	key := edgeKey(caller, callee)
	stats, exists := t.edgeStats[key]
	if !exists {
		stats = &edgeStats{
			caller:        caller,
			callee:        callee,
			minExecTime:   executionTime,
			maxExecTime:   executionTime,
			avgCalculator: NewAveragingCalculator(t.averagingMethod, t.GetAverageMethodConfig()),
		}
		t.edgeStats[key] = stats
	}

	stats.count++
	stats.totalExecTime += executionTime
	stats.avgCalculator.Add(executionTime)

	if executionTime < stats.minExecTime || stats.count == 1 {
		stats.minExecTime = executionTime
	}
	if executionTime > stats.maxExecTime {
		stats.maxExecTime = executionTime
	}

	// Update caller-callee mappings
	if t.callerToCallees[caller] == nil {
		t.callerToCallees[caller] = make(map[string]bool)
	}
	t.callerToCallees[caller][callee] = true

	if t.calleeToCallers[callee] == nil {
		t.calleeToCallers[callee] = make(map[string]bool)
	}
	t.calleeToCallers[callee][caller] = true

	t.logger.Debug("recorded edge",
		zap.String("caller", caller),
		zap.String("callee", callee),
		zap.String("requestID", requestID),
		zap.String("executionID", callerExecutionID),
		zap.Duration("edgeExecutionTime", executionTime))
}

// EndExecution marks the end of a function execution, records function stats, and cleans up execution context
func (t *CallGraphTracker) EndExecution(functionName string, requestID string, executionID string, timestamp time.Time) {
	if !t.config.Enabled {
		return
	}

	if functionName == "" || requestID == "" || executionID == "" {
		t.logger.Warn("functionName, requestID, or executionID is empty",
			zap.String("function", functionName),
			zap.String("requestID", requestID),
			zap.String("executionID", executionID))
		return
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()

	// Look up start time and calculate execution time
	var execTime time.Duration
	var startTime time.Time
	if requestCtx, ok := t.executionContexts[requestID]; ok {
		if ctx, ok := requestCtx[executionID]; ok {
			if ctx.functionName != functionName {
				t.logger.Warn("execution context function mismatch",
					zap.String("function", functionName),
					zap.String("requestID", requestID),
					zap.String("executionID", executionID))
				return
			}
			startTime = ctx.startTime
			execTime = timestamp.Sub(startTime)
		} else {
			t.logger.Warn("execution context not found for function",
				zap.String("function", functionName),
				zap.String("requestID", requestID),
				zap.String("executionID", executionID))
			return
		}
	} else {
		t.logger.Warn("request context not found for requestID", zap.String("requestID", requestID))
		return
	}

	// Record function stats (only if we have a valid start time)
	if !startTime.IsZero() && execTime >= 0 {
		stats, exists := t.functionStats[functionName]
		if !exists {
			stats = &FunctionStats{
				Name:                 functionName,
				MinExecutionTime:     execTime,
				MaxExecutionTime:     execTime,
				avgExecCalculator:    NewAveragingCalculator(t.averagingMethod, t.GetAverageMethodConfig()),
				avgScaleUpCalculator: NewAveragingCalculator(t.averagingMethod, t.GetAverageMethodConfig()),
			}
			t.functionStats[functionName] = stats
		}

		stats.TotalCalls++
		stats.TotalExecutionTime += execTime
		stats.LastExecutionTime = execTime
		stats.LastCalledAt = startTime
		stats.avgExecCalculator.Add(execTime)

		if execTime < stats.MinExecutionTime || stats.MinExecutionTime == 0 {
			stats.MinExecutionTime = execTime
		}
		if execTime > stats.MaxExecutionTime {
			stats.MaxExecutionTime = execTime
		}

		t.logger.Debug("recorded function execution",
			zap.String("function", functionName),
			zap.String("requestID", requestID),
			zap.Duration("executionTime", execTime))
	}

	// Clean up execution context
	if requestCtx, ok := t.executionContexts[requestID]; ok {
		delete(requestCtx, executionID)

		// If this was the last function in this request, clean up the entire request context
		if len(requestCtx) == 0 {
			delete(t.executionContexts, requestID)
			delete(t.contextLastAccess, requestID)
		}
	}

	t.logger.Debug("ended execution",
		zap.String("function", functionName),
		zap.String("requestID", requestID),
		zap.String("executionID", executionID),
		zap.Time("timestamp", timestamp))
}

// contextCleanupLoop runs periodically to clean up stale execution contexts
func (t *CallGraphTracker) contextCleanupLoop() {
	// Get cleanup interval, use default if not set
	interval := t.config.ContextCleanupInterval
	if interval <= 0 {
		interval = DefaultContextCleanupInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer close(t.cleanupDone)

	for {
		select {
		case <-ticker.C:
			t.cleanupStaleContexts()
		case <-t.cleanupStop:
			return
		}
	}
}

// cleanupStaleContexts removes execution contexts that have exceeded their TTL
func (t *CallGraphTracker) cleanupStaleContexts() {
	ttl := t.config.ContextTTL
	if ttl <= 0 {
		ttl = DefaultContextTTL
	}

	now := time.Now()
	cutoff := now.Add(-ttl)

	t.mutex.Lock()
	defer t.mutex.Unlock()

	cleanedCount := 0
	for requestID, lastAccess := range t.contextLastAccess {
		if lastAccess.Before(cutoff) {
			delete(t.executionContexts, requestID)
			delete(t.contextLastAccess, requestID)
			cleanedCount++
		}
	}

	if cleanedCount > 0 {
		t.logger.Debug("cleaned up stale execution contexts",
			zap.Int("count", cleanedCount),
			zap.Duration("ttl", ttl))
	}
}

// Start initializes and starts the tracker's background goroutines
func (t *CallGraphTracker) Start() {
	if t.config.Enabled {
		// Start context cleanup goroutine
		t.logger.Info("callgraph tracker started")
		go t.contextCleanupLoop()
	}
}

// Stop gracefully stops the tracker's background goroutines
func (t *CallGraphTracker) Stop() {
	if t.config.Enabled {
		close(t.cleanupStop)
		<-t.cleanupDone
		t.logger.Info("callgraph tracker stopped")
	}
}

// RecordScaleDown records a scale-down event for a function
func (t *CallGraphTracker) RecordScaleDown(functionName string, timestamp time.Time, duration time.Duration) {
	if !t.config.Enabled {
		return
	}

	if functionName == "" {
		return
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()

	stats, exists := t.functionStats[functionName]
	if !exists {
		t.logger.Warn("function stats not found for scale down", zap.String("function", functionName))
		return
	}

	stats.TotalScaleDowns++
}

func (t *CallGraphTracker) RecordScaleUp(functionName string, timestamp time.Time, duration time.Duration, cold bool) {
	if !t.config.Enabled {
		return
	}

	if functionName == "" {
		return
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()

	stats, exists := t.functionStats[functionName]
	if !exists {
		// Auto-create function stats entry for cold start tracking
		// This allows tracking cold starts for newly deployed functions
		stats = &FunctionStats{
			Name:                 functionName,
			avgExecCalculator:    NewAveragingCalculator(t.averagingMethod, t.GetAverageMethodConfig()),
			avgScaleUpCalculator: NewAveragingCalculator(t.averagingMethod, t.GetAverageMethodConfig()),
		}
		t.functionStats[functionName] = stats
	}

	if cold {
		stats.TotalColdStarts++
		stats.LastColdStartAt = timestamp
		stats.LastColdStartDuration = duration
	} else {
		stats.TotalPrewarms++
		stats.LastPrewarmAt = timestamp
		stats.LastPrewarmDuration = duration
	}
	stats.TotalScaleUps++
	stats.avgScaleUpCalculator.Add(duration)
}

// GetColdStartStats returns cold start statistics for a specific function
func (t *CallGraphTracker) GetColdStartStats(functionName string) (coldStarts int, lastColdStartAt time.Time, lastColdStartDuration time.Duration, ok bool) {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	stats, exists := t.functionStats[functionName]
	if !exists {
		return 0, time.Time{}, 0, false
	}

	return stats.TotalColdStarts, stats.LastColdStartAt, stats.LastColdStartDuration, true
}

// GetColdStartAverage returns the calculated average cold start duration for a function
func (t *CallGraphTracker) GetColdStartAverage(functionName string) time.Duration {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	stats, exists := t.functionStats[functionName]
	if !exists {
		return 0
	}

	return stats.avgScaleUpCalculator.Average()
}

// GetCallGraph returns the complete call graph with aggregated data
func (t *CallGraphTracker) GetCallGraph() CallGraph {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	edges := make([]AggregatedEdge, 0, len(t.edgeStats))
	for _, stats := range t.edgeStats {
		edges = append(edges, AggregatedEdge{
			Caller:             stats.caller,
			Callee:             stats.callee,
			Count:              stats.count,
			TotalExecutionTime: stats.totalExecTime,
			AvgExecutionTime:   stats.avgCalculator.Average(),
			MinExecutionTime:   stats.minExecTime,
			MaxExecutionTime:   stats.maxExecTime,
		})
	}

	// Sort edges for consistent output
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Caller != edges[j].Caller {
			return edges[i].Caller < edges[j].Caller
		}
		return edges[i].Callee < edges[j].Callee
	})

	functions := t.getAllFunctionStatsNoLock()

	totalCalls := 0
	for _, stats := range t.functionStats {
		totalCalls += stats.TotalCalls
	}

	return CallGraph{
		Edges:         edges,
		Functions:     functions,
		TotalCalls:    totalCalls,
		RecordedSince: t.startTime,
	}
}

// GetFunctionStats returns statistics for a specific function
func (t *CallGraphTracker) GetFunctionStats(functionName string) (FunctionStats, bool) {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	stats, ok := t.functionStats[functionName]
	if !ok {
		return FunctionStats{}, false
	}

	// Return a copy with calculated averages
	result := *stats
	result.AvgExecutionTime = stats.avgExecCalculator.Average()
	result.AvgColdStartDuration = stats.avgScaleUpCalculator.Average()
	return result, true
}

// GetAllFunctionStats returns statistics for all functions
func (t *CallGraphTracker) GetAllFunctionStats() map[string]FunctionStats {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	return t.getAllFunctionStatsNoLock()
}

// getAllFunctionStatsNoLock is like GetAllFunctionStats but assumes the lock is already held
func (t *CallGraphTracker) getAllFunctionStatsNoLock() map[string]FunctionStats {
	result := make(map[string]FunctionStats, len(t.functionStats))
	for name, stats := range t.functionStats {
		// Return a copy with calculated averages
		copy := *stats
		copy.AvgExecutionTime = stats.avgExecCalculator.Average()
		copy.AvgColdStartDuration = stats.avgScaleUpCalculator.Average()
		result[name] = copy
	}
	return result
}

// GetCallees returns all functions that the given function calls
func (t *CallGraphTracker) GetCallees(functionName string) []string {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	callees := make([]string, 0)
	if calleeMap, ok := t.callerToCallees[functionName]; ok {
		for callee := range calleeMap {
			callees = append(callees, callee)
		}
	}
	sort.Strings(callees)
	return callees
}

// GetCallers returns all functions that call the given function
func (t *CallGraphTracker) GetCallers(functionName string) []string {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	callers := make([]string, 0)
	if callerMap, ok := t.calleeToCallers[functionName]; ok {
		for caller := range callerMap {
			callers = append(callers, caller)
		}
	}
	sort.Strings(callers)
	return callers
}

// GetAverageExecutionTime returns the calculated average execution time for a function in nanoseconds
func (t *CallGraphTracker) GetAverageExecutionTime(functionName string) int64 {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	if stats, ok := t.functionStats[functionName]; ok {
		return int64(stats.avgExecCalculator.Average())
	}
	return 0
}

// GetEdgeStats returns the aggregated stats for a specific caller->callee edge
func (t *CallGraphTracker) GetEdgeStats(caller, callee string) (AggregatedEdge, bool) {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	key := edgeKey(caller, callee)
	stats, ok := t.edgeStats[key]
	if !ok {
		return AggregatedEdge{}, false
	}

	return AggregatedEdge{
		Caller:             stats.caller,
		Callee:             stats.callee,
		Count:              stats.count,
		TotalExecutionTime: stats.totalExecTime,
		AvgExecutionTime:   stats.avgCalculator.Average(),
		MinExecutionTime:   stats.minExecTime,
		MaxExecutionTime:   stats.maxExecTime,
	}, true
}

// Clear clears all recorded data
func (t *CallGraphTracker) Clear() {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	t.edgeStats = make(map[string]*edgeStats)
	t.functionStats = make(map[string]*FunctionStats)
	t.callerToCallees = make(map[string]map[string]bool)
	t.calleeToCallers = make(map[string]map[string]bool)
	t.executionContexts = make(map[string]map[string]executionContext)
	t.contextLastAccess = make(map[string]time.Time)
	t.startTime = time.Now()
}

// ResetFunctionStats clears stats for a redeployed function while preserving edge structure.
// This ensures fresh timing data after redeployment while maintaining workflow knowledge.
// The initial cold start recorded after redeployment will provide fresh prewarm data.
func (t *CallGraphTracker) ResetFunctionStats(functionName string) {
	if !t.config.Enabled || functionName == "" {
		return
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()

	stats, exists := t.functionStats[functionName]
	if !exists {
		t.logger.Debug("function stats not found for reset, skipping",
			zap.String("function", functionName))
		return
	}

	// Track reset for observability
	stats.TotalResets++
	stats.LastResetAt = time.Now()

	// Clear all timing and count stats
	stats.TotalCalls = 0
	stats.TotalColdStarts = 0
	stats.TotalScaleUps = 0
	stats.TotalPrewarms = 0
	stats.TotalScaleDowns = 0
	stats.TotalExecutionTime = 0
	stats.MinExecutionTime = 0
	stats.MaxExecutionTime = 0
	stats.LastExecutionTime = 0
	stats.LastCalledAt = time.Time{}
	stats.LastColdStartAt = time.Time{}
	stats.LastColdStartDuration = 0
	stats.LastPrewarmAt = time.Time{}
	stats.LastPrewarmDuration = 0
	stats.AvgExecutionTime = 0
	stats.AvgColdStartDuration = 0

	// Reset averaging calculators for fresh data
	stats.avgExecCalculator = NewAveragingCalculator(t.averagingMethod, t.GetAverageMethodConfig())
	stats.avgScaleUpCalculator = NewAveragingCalculator(t.averagingMethod, t.GetAverageMethodConfig())

	t.logger.Info("reset function stats for redeployment",
		zap.String("function", functionName),
		zap.Int("totalResets", stats.TotalResets))
}

// ClearFunctionData removes all callgraph data for a deleted function.
// This includes function stats, all edges where the function is caller or callee,
// and all caller/callee mappings.
func (t *CallGraphTracker) ClearFunctionData(functionName string) {
	if !t.config.Enabled || functionName == "" {
		return
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()

	// Remove function stats
	delete(t.functionStats, functionName)

	// Remove edges where function is caller and clean up callee's caller list
	if callees, ok := t.callerToCallees[functionName]; ok {
		for callee := range callees {
			key := edgeKey(functionName, callee)
			delete(t.edgeStats, key)
			// Remove from callee's caller list
			if callerSet, ok := t.calleeToCallers[callee]; ok {
				delete(callerSet, functionName)
			}
		}
		delete(t.callerToCallees, functionName)
	}

	// Remove edges where function is callee and clean up caller's callee list
	if callers, ok := t.calleeToCallers[functionName]; ok {
		for caller := range callers {
			key := edgeKey(caller, functionName)
			delete(t.edgeStats, key)
			// Remove from caller's callee list
			if calleeSet, ok := t.callerToCallees[caller]; ok {
				delete(calleeSet, functionName)
			}
		}
		delete(t.calleeToCallers, functionName)
	}

	// Clean up any active execution contexts for this function
	for requestID, ctx := range t.executionContexts {
		for executionID, execCtx := range ctx {
			if execCtx.functionName == functionName {
				delete(ctx, executionID)
			}
		}
		if len(ctx) == 0 {
			delete(t.executionContexts, requestID)
			delete(t.contextLastAccess, requestID)
		}
	}

	t.logger.Info("cleared function data for deletion",
		zap.String("function", functionName))
}

// EdgeCount returns the number of unique edges
func (t *CallGraphTracker) EdgeCount() int {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	return len(t.edgeStats)
}

// FunctionCount returns the number of unique functions tracked
func (t *CallGraphTracker) FunctionCount() int {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	return len(t.functionStats)
}

// GetPrewarmTargets returns a list of functions that should be prewarmed
// when the given function starts execution.
// It analyzes the call graph to find downstream functions that could benefit from prewarming.
func (t *CallGraphTracker) GetPrewarmTargets(functionName string) []PrewarmTarget {
	if !t.PrewarmEnabled() {
		return nil
	}

	t.mutex.RLock()
	defer t.mutex.RUnlock()

	targets := make([]PrewarmTarget, 0)

	// Get all functions this function directly calls
	callees, ok := t.callerToCallees[functionName]
	if !ok {
		return targets
	}

	// Calculate total calls from this function (for confidence calculation)
	totalCallsFromFunction := 0
	for callee := range callees {
		key := edgeKey(functionName, callee)
		if stats, exists := t.edgeStats[key]; exists {
			totalCallsFromFunction += stats.count
		}
	}

	for callee := range callees {
		key := edgeKey(functionName, callee)
		edgeStats, exists := t.edgeStats[key]
		if !exists {
			continue
		}

		calleeStats, calleeExists := t.functionStats[callee]
		if !calleeExists {
			continue
		}

		if edgeStats.count <= 0 {
			continue
		}

		// Skip if callee has no cold start data
		if calleeStats.TotalColdStarts <= 0 {
			continue
		}

		avgEdgeTime := edgeStats.avgCalculator.Average()
		avgColdStartTime := calleeStats.avgScaleUpCalculator.Average()

		if avgColdStartTime == 0 {
			continue
		}

		// LeadTime represents the expected time until the callee is invoked
		// (i.e., the average edge time from caller start to callee call).
		// The prewarm scheduler in other components should subtract cold start time to
		// determine when to actually trigger the prewarm.
		leadTime := max(avgEdgeTime, 0)
		targets = append(targets, PrewarmTarget{
			FunctionName: callee,
			LeadTime:     leadTime,
		})
	}

	return targets
}

// ====== Implementation for Serializer interface ======

// ToJSON returns the call graph as JSON bytes
func (t *CallGraphTracker) ToJSON() ([]byte, error) {
	graph := t.GetCallGraph()
	return json.Marshal(graph)
}

// FromJSON loads call graph data from JSON bytes
// Note: This replaces all current data and resets averaging calculators
func (t *CallGraphTracker) FromJSON(data []byte) error {
	var graph CallGraph
	if err := json.Unmarshal(data, &graph); err != nil {
		return err
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()

	// Clear existing data
	t.edgeStats = make(map[string]*edgeStats)
	t.functionStats = make(map[string]*FunctionStats)
	t.callerToCallees = make(map[string]map[string]bool)
	t.calleeToCallers = make(map[string]map[string]bool)
	t.startTime = graph.RecordedSince

	// Restore edge stats
	for _, edge := range graph.Edges {
		key := edgeKey(edge.Caller, edge.Callee)
		t.edgeStats[key] = &edgeStats{
			caller:        edge.Caller,
			callee:        edge.Callee,
			count:         edge.Count,
			totalExecTime: edge.TotalExecutionTime,
			minExecTime:   edge.MinExecutionTime,
			maxExecTime:   edge.MaxExecutionTime,
			avgCalculator: NewAveragingCalculator(t.averagingMethod, t.GetAverageMethodConfig()),
		}

		// Update caller-callee mappings
		if t.callerToCallees[edge.Caller] == nil {
			t.callerToCallees[edge.Caller] = make(map[string]bool)
		}
		t.callerToCallees[edge.Caller][edge.Callee] = true

		if t.calleeToCallers[edge.Callee] == nil {
			t.calleeToCallers[edge.Callee] = make(map[string]bool)
		}
		t.calleeToCallers[edge.Callee][edge.Caller] = true
	}

	// Restore function stats
	for name, stats := range graph.Functions {
		statsCopy := stats
		statsCopy.avgExecCalculator = NewAveragingCalculator(t.averagingMethod, t.GetAverageMethodConfig())
		statsCopy.avgScaleUpCalculator = NewAveragingCalculator(t.averagingMethod, t.GetAverageMethodConfig())
		t.functionStats[name] = &statsCopy
	}

	return nil
}

// GetCallPaths returns all unique call paths starting from external calls
// This properly handles DAG (Directed Acyclic Graph) topologies with branches
func (t *CallGraphTracker) GetCallPaths() []CallPath {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	paths := make([]CallPath, 0)

	// Find all entry points (functions called externally)
	entryPoints := make([]string, 0)
	if callees, ok := t.callerToCallees[""]; ok {
		for callee := range callees {
			entryPoints = append(entryPoints, callee)
		}
	}
	sort.Strings(entryPoints)

	// DFS to find all paths from each entry point
	// Use a path-local visited set to detect cycles within a single path
	for _, entry := range entryPoints {
		pathVisited := make(map[string]bool)
		t.findAllPaths(entry, []string{}, pathVisited, &paths)
	}

	return paths
}

// findAllPaths performs DFS to find all paths in a DAG
// pathVisited tracks nodes in the current path only (to detect cycles)
// Each branch gets its own exploration without blocking other branches
func (t *CallGraphTracker) findAllPaths(current string, currentPath []string, pathVisited map[string]bool, paths *[]CallPath) {
	// Cycle detection: if we've already visited this node in the current path, skip
	if pathVisited[current] {
		return
	}

	// Add current node to path
	newPath := make([]string, len(currentPath)+1)
	copy(newPath, currentPath)
	newPath[len(currentPath)] = current

	// Create a copy of pathVisited for this branch
	newPathVisited := make(map[string]bool)
	for k, v := range pathVisited {
		newPathVisited[k] = v
	}
	newPathVisited[current] = true

	callees := t.callerToCallees[current]
	if len(callees) == 0 {
		// Leaf node - record this complete path
		pathCopy := make([]string, len(newPath))
		copy(pathCopy, newPath)

		// Calculate total execution time for this path
		var totalExecTime time.Duration
		for _, fn := range pathCopy {
			if stats, ok := t.functionStats[fn]; ok {
				totalExecTime += stats.avgExecCalculator.Average()
			}
		}

		*paths = append(*paths, CallPath{
			Path:               pathCopy,
			TotalExecutionTime: totalExecTime,
			Count:              1,
		})
	} else {
		// Explore all callees (branches)
		// Sort for consistent ordering
		sortedCallees := make([]string, 0, len(callees))
		for callee := range callees {
			sortedCallees = append(sortedCallees, callee)
		}
		sort.Strings(sortedCallees)

		for _, callee := range sortedCallees {
			t.findAllPaths(callee, newPath, newPathVisited, paths)
		}
	}

	// No need to unmark since we use a copy of the map for each branch
}

// GetEntryPoints returns all functions that are called from external sources
func (t *CallGraphTracker) GetEntryPoints() []string {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	entryPoints := make([]string, 0)
	if callees, ok := t.callerToCallees[""]; ok {
		for callee := range callees {
			entryPoints = append(entryPoints, callee)
		}
	}
	sort.Strings(entryPoints)
	return entryPoints
}

// GetLeafFunctions returns all functions that don't call any other functions
func (t *CallGraphTracker) GetLeafFunctions() []string {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	leaves := make([]string, 0)
	for fn := range t.functionStats {
		if callees, ok := t.callerToCallees[fn]; !ok || len(callees) == 0 {
			leaves = append(leaves, fn)
		}
	}
	sort.Strings(leaves)
	return leaves
}

// GetDownstreamFunctions returns all functions reachable from the given function (BFS)
func (t *CallGraphTracker) GetDownstreamFunctions(functionName string) []string {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	visited := make(map[string]bool)
	queue := []string{functionName}
	result := make([]string, 0)

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if visited[current] {
			continue
		}
		visited[current] = true

		if current != functionName {
			result = append(result, current)
		}

		if callees, ok := t.callerToCallees[current]; ok {
			for callee := range callees {
				if !visited[callee] {
					queue = append(queue, callee)
				}
			}
		}
	}

	sort.Strings(result)
	return result
}

// GetUpstreamFunctions returns all functions that can reach the given function (BFS backwards)
func (t *CallGraphTracker) GetUpstreamFunctions(functionName string) []string {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	visited := make(map[string]bool)
	queue := []string{functionName}
	result := make([]string, 0)

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if visited[current] {
			continue
		}
		visited[current] = true

		if current != functionName && current != "" {
			result = append(result, current)
		}

		if callers, ok := t.calleeToCallers[current]; ok {
			for caller := range callers {
				if !visited[caller] && caller != "" {
					queue = append(queue, caller)
				}
			}
		}
	}

	sort.Strings(result)
	return result
}
