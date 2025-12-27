package callgraph

import (
	"encoding/json"
	"errors"
	"sort"
	"time"

	"go.uber.org/zap"
)

var ErrInvalidMaxEdges = errors.New("MaxEdges must be -1 (unlimited) or a positive integer")

// New creates a new CallGraphTracker with default configuration
func New(options ...Option) *CallGraphTracker {
	tracker := &CallGraphTracker{
		config:            DefaultConfig(),
		logger:            DefaultLogger(),
		edges:             make([]CallEdge, 0),
		edgeStats:         make(map[string]*edgeStats),
		functionStats:     make(map[string]*FunctionStats),
		callerToCallees:   make(map[string]map[string]bool),
		calleeToCallers:   make(map[string]map[string]bool),
		executionContexts: make(map[string]map[string]time.Time),
		startTime:         time.Now(),
	}
	// Default to SMA
	WithSMA(&SMAConfig{
		WindowSize: DEFAULT_SMA_WINDOW_SIZE,
	})(tracker)
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
	}
}

func WithSMA(smaConfig *SMAConfig) Option {
	return func(tracker *CallGraphTracker) {
		tracker.averagingMethod = SimpleMovingAverage
		tracker.smaConfig = smaConfig
	}
}

func WithEMA(emaConfig *EMAConfig) Option {
	return func(tracker *CallGraphTracker) {
		tracker.averagingMethod = ExponentialMovingAverage
		tracker.emaConfig = emaConfig
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
		Enabled:  true,
		MaxEdges: -1,
	}
}

func validateConfig(config *Config) error {
	if config.MaxEdges == 0 || config.MaxEdges < -1 {
		return ErrInvalidMaxEdges
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

// RecordEdge records a call from caller to callee with the given execution time in nanoseconds
// caller is "" (empty string) for external calls
func (t *CallGraphTracker) RecordEdge(caller, callee string, executionTime time.Duration) {
	if !t.config.Enabled {
		return
	}

	// Validate: callee cannot be empty
	if callee == "" {
		return
	}

	// Prevent self-loops: a function cannot call itself
	if caller != "" && caller == callee {
		return
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()

	now := time.Now()

	// Record detailed edge
	edge := CallEdge{
		Caller:        caller,
		Callee:        callee,
		ExecutionTime: executionTime,
		Timestamp:     now,
	}

	// Manage edge storage with limit
	if t.config.MaxEdges > 0 && len(t.edges) >= t.config.MaxEdges {
		// Remove oldest edges (FIFO)
		t.edges = t.edges[1:]
	}
	t.edges = append(t.edges, edge)

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

	if executionTime < stats.minExecTime {
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
}

// RecordEdgeCall records a call without execution time (uses 0)
func (t *CallGraphTracker) RecordEdgeCall(caller, callee string) {
	if !t.config.Enabled {
		return
	}
	t.RecordEdge(caller, callee, 0)
}

// StartExecution marks the beginning of a function execution for a specific request
func (t *CallGraphTracker) StartExecution(functionName string, requestID string, timestamp time.Time) {
	if !t.config.Enabled {
		return
	}

	if functionName == "" || requestID == "" {
		return
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()

	// Initialize request context if needed
	if t.executionContexts[requestID] == nil {
		t.executionContexts[requestID] = make(map[string]time.Time)
	}

	// Store start time for this function in this request
	t.executionContexts[requestID][functionName] = timestamp

	t.logger.Debug("started execution",
		zap.String("function", functionName),
		zap.String("requestID", requestID),
		zap.Time("timestamp", timestamp))
}

// RecordCall records when caller invokes callee and automatically calculates edge execution time
func (t *CallGraphTracker) RecordCall(caller, callee string, requestID string, timestamp time.Time) {
	if !t.config.Enabled {
		return
	}

	// Validate: callee cannot be empty
	if callee == "" {
		return
	}

	// Prevent self-loops: a function cannot call itself
	if caller != "" && caller == callee {
		return
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()

	var executionTime time.Duration

	// If this is an internal call (caller is not empty), calculate edge execution time
	if caller != "" && requestID != "" {
		// Look up when caller started in this request
		if requestCtx, ok := t.executionContexts[requestID]; ok {
			if startTime, ok := requestCtx[caller]; ok {
				executionTime = timestamp.Sub(startTime)
				t.logger.Debug("calculated edge execution time",
					zap.String("caller", caller),
					zap.String("callee", callee),
					zap.String("requestID", requestID),
					zap.Duration("executionTime", executionTime))
			} else {
				t.logger.Warn("caller start time not found",
					zap.String("caller", caller),
					zap.String("callee", callee),
					zap.String("requestID", requestID))
				// Can't calculate edge time, skip recording
				return
			}
		} else {
			t.logger.Warn("request context not found",
				zap.String("requestID", requestID),
				zap.String("caller", caller),
				zap.String("callee", callee))
			// Can't calculate edge time, skip recording
			return
		}
	}

	// Record detailed edge
	edge := CallEdge{
		Caller:        caller,
		Callee:        callee,
		ExecutionTime: executionTime,
		Timestamp:     timestamp,
	}

	// Manage edge storage with limit
	if t.config.MaxEdges > 0 && len(t.edges) >= t.config.MaxEdges {
		// Remove oldest edges (FIFO)
		t.edges = t.edges[1:]
	}
	t.edges = append(t.edges, edge)

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

	t.logger.Debug("recorded call",
		zap.String("caller", caller),
		zap.String("callee", callee),
		zap.String("requestID", requestID),
		zap.Duration("edgeExecutionTime", executionTime))
}

// EndExecution marks the end of a function execution and cleans up execution context
func (t *CallGraphTracker) EndExecution(functionName string, requestID string, timestamp time.Time) {
	if !t.config.Enabled {
		return
	}

	if functionName == "" || requestID == "" {
		return
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()

	// Clean up execution context
	if requestCtx, ok := t.executionContexts[requestID]; ok {
		delete(requestCtx, functionName)

		// If this was the last function in this request, clean up the entire request context
		if len(requestCtx) == 0 {
			delete(t.executionContexts, requestID)
		}
	}

	t.logger.Debug("ended execution",
		zap.String("function", functionName),
		zap.String("requestID", requestID),
		zap.Time("timestamp", timestamp))
}

// RecordColdStart records a cold start event for a function
func (t *CallGraphTracker) RecordColdStart(functionName string, timestamp time.Time, coldStartDuration time.Duration) {
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
		return
	}

	stats.TotalColdStarts++
	stats.LastColdStartAt = timestamp
	stats.LastColdStartDuration = coldStartDuration
	stats.avgColdStartCalculator.Add(coldStartDuration)
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

	return stats.avgColdStartCalculator.Average()
}

// RecordFuncExec records a function whole execution with timing information
func (t *CallGraphTracker) RecordFuncExec(functionName string, timestamp time.Time, execTime time.Duration) {
	if !t.config.Enabled {
		return
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()

	stats, exists := t.functionStats[functionName]
	if !exists {
		stats = &FunctionStats{
			Name:                   functionName,
			MinExecutionTime:       execTime,
			MaxExecutionTime:       execTime,
			avgCalculator:          NewAveragingCalculator(t.averagingMethod, t.GetAverageMethodConfig()),
			avgColdStartCalculator: NewAveragingCalculator(t.averagingMethod, t.GetAverageMethodConfig()),
		}
		t.functionStats[functionName] = stats
	}

	stats.TotalCalls++
	stats.TotalExecutionTime += execTime
	stats.LastExecutionTime = execTime
	stats.LastCalledAt = timestamp
	stats.avgCalculator.Add(execTime)

	if execTime < stats.MinExecutionTime || stats.MinExecutionTime == 0 {
		stats.MinExecutionTime = execTime
	}
	if execTime > stats.MaxExecutionTime {
		stats.MaxExecutionTime = execTime
	}
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
	result.AvgExecutionTime = stats.avgCalculator.Average()
	result.AvgColdStartDuration = stats.avgColdStartCalculator.Average()
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
		copy.AvgExecutionTime = stats.avgCalculator.Average()
		copy.AvgColdStartDuration = stats.avgColdStartCalculator.Average()
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
		return int64(stats.avgCalculator.Average())
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

// GetEdges returns all recorded detailed edges
func (t *CallGraphTracker) GetEdges() []CallEdge {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	result := make([]CallEdge, len(t.edges))
	copy(result, t.edges)
	return result
}

// Clear clears all recorded data
func (t *CallGraphTracker) Clear() {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	t.edges = make([]CallEdge, 0)
	t.edgeStats = make(map[string]*edgeStats)
	t.functionStats = make(map[string]*FunctionStats)
	t.callerToCallees = make(map[string]map[string]bool)
	t.calleeToCallers = make(map[string]map[string]bool)
	t.startTime = time.Now()
}

// EdgeCount returns the total number of recorded detailed edges
func (t *CallGraphTracker) EdgeCount() int {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	return len(t.edges)
}

// FunctionCount returns the number of unique functions tracked
func (t *CallGraphTracker) FunctionCount() int {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	return len(t.functionStats)
}

// UniqueEdgeCount returns the number of unique caller->callee edges
func (t *CallGraphTracker) UniqueEdgeCount() int {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	return len(t.edgeStats)
}

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
	t.edges = make([]CallEdge, 0)
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
		statsCopy.avgCalculator = NewAveragingCalculator(t.averagingMethod, t.GetAverageMethodConfig())
		statsCopy.avgColdStartCalculator = NewAveragingCalculator(t.averagingMethod, t.GetAverageMethodConfig())
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
				totalExecTime += stats.avgCalculator.Average()
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

// GetPathsContaining returns all paths that contain the given function
func (t *CallGraphTracker) GetPathsContaining(functionName string) []CallPath {
	allPaths := t.GetCallPaths()
	result := make([]CallPath, 0)

	for _, path := range allPaths {
		for _, fn := range path.Path {
			if fn == functionName {
				result = append(result, path)
				break
			}
		}
	}

	return result
}

// GetLongestPath returns the longest call path by number of functions
func (t *CallGraphTracker) GetLongestPath() CallPath {
	paths := t.GetCallPaths()
	if len(paths) == 0 {
		return CallPath{}
	}

	longest := paths[0]
	for _, path := range paths[1:] {
		if len(path.Path) > len(longest.Path) {
			longest = path
		}
	}
	return longest
}

// GetSlowestPath returns the call path with the highest total execution time
func (t *CallGraphTracker) GetSlowestPath() CallPath {
	paths := t.GetCallPaths()
	if len(paths) == 0 {
		return CallPath{}
	}

	slowest := paths[0]
	for _, path := range paths[1:] {
		if path.TotalExecutionTime > slowest.TotalExecutionTime {
			slowest = path
		}
	}
	return slowest
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

// GetSubgraph returns a new CallGraph containing only the specified functions and their edges
func (t *CallGraphTracker) GetSubgraph(functionNames []string) CallGraph {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	fnSet := make(map[string]bool)
	for _, fn := range functionNames {
		fnSet[fn] = true
	}

	edges := make([]AggregatedEdge, 0)
	for _, stats := range t.edgeStats {
		// Include edge if both caller and callee are in the set (or caller is external)
		callerOk := stats.caller == "" || fnSet[stats.caller]
		calleeOk := fnSet[stats.callee]
		if callerOk && calleeOk {
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
	}

	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Caller != edges[j].Caller {
			return edges[i].Caller < edges[j].Caller
		}
		return edges[i].Callee < edges[j].Callee
	})

	functions := make(map[string]FunctionStats)
	totalCalls := 0
	for name, stats := range t.functionStats {
		if fnSet[name] {
			// Return a copy with calculated averages
			copy := *stats
			copy.AvgExecutionTime = stats.avgCalculator.Average()
			copy.AvgColdStartDuration = stats.avgColdStartCalculator.Average()
			functions[name] = copy
			totalCalls += stats.TotalCalls
		}
	}

	return CallGraph{
		Edges:         edges,
		Functions:     functions,
		TotalCalls:    totalCalls,
		RecordedSince: t.startTime,
	}
}

// GetAggregatedGraph returns the aggregated call graph as a map (deprecated, use GetCallGraph instead)
func (t *CallGraphTracker) GetAggregatedGraph() map[string]map[string]int {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	result := make(map[string]map[string]int)
	for key, stats := range t.edgeStats {
		_ = key // key is not used, we extract caller/callee from stats
		if result[stats.caller] == nil {
			result[stats.caller] = make(map[string]int)
		}
		result[stats.caller][stats.callee] = stats.count
	}
	return result
}
