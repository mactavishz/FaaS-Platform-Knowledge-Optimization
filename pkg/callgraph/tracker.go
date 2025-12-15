package callgraph

import (
	"encoding/json"
	"sort"
	"sync"
	"time"
)

// edgeKey creates a unique key for a caller->callee edge
func edgeKey(caller, callee string) string {
	return caller + "->" + callee
}

// edgeStats holds internal statistics for an edge
type edgeStats struct {
	caller        string
	callee        string
	count         int
	totalExecTime time.Duration
	minExecTime   time.Duration
	maxExecTime   time.Duration
	avgCalculator AveragingCalculator
}

// functionInternalStats holds internal statistics for a function
type functionInternalStats struct {
	name          string
	totalCalls    int
	totalExecTime time.Duration
	minExecTime   time.Duration
	maxExecTime   time.Duration
	lastExecTime  time.Duration
	lastCalledAt  time.Time
	avgCalculator AveragingCalculator
}

// CallGraphTracker tracks function call relationships with execution time
type CallGraphTracker struct {
	config Config

	// Detailed edges (recent calls only based on MaxEdges)
	edges []CallEdge

	// Edge statistics: edgeKey -> stats
	edgeStats map[string]*edgeStats

	// Function statistics: functionName -> stats
	functionStats map[string]*functionInternalStats

	// Caller to callees mapping for quick lookup
	callerToCallees map[string]map[string]bool

	// Callee to callers mapping for quick lookup
	calleeToCamllers map[string]map[string]bool

	// When tracking started
	startTime time.Time

	mutex sync.RWMutex
}

// New creates a new CallGraphTracker with default configuration
func New() *CallGraphTracker {
	return NewWithConfig(DefaultConfig())
}

// NewWithConfig creates a new CallGraphTracker with the given configuration
func NewWithConfig(config Config) *CallGraphTracker {
	return &CallGraphTracker{
		config:           config,
		edges:            make([]CallEdge, 0),
		edgeStats:        make(map[string]*edgeStats),
		functionStats:    make(map[string]*functionInternalStats),
		callerToCallees:  make(map[string]map[string]bool),
		calleeToCamllers: make(map[string]map[string]bool),
		startTime:        time.Now(),
	}
}

// Record records a call from caller to callee with the given execution time in nanoseconds
// caller is "" (empty string) for external calls
func (t *CallGraphTracker) Record(caller, callee string, executionTimeNs int64) {
	execTime := time.Duration(executionTimeNs)
	t.mutex.Lock()
	defer t.mutex.Unlock()

	now := time.Now()

	// Record detailed edge
	edge := CallEdge{
		Caller:        caller,
		Callee:        callee,
		ExecutionTime: execTime,
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
			minExecTime:   execTime,
			maxExecTime:   execTime,
			avgCalculator: NewAveragingCalculator(t.config),
		}
		t.edgeStats[key] = stats
	}

	stats.count++
	stats.totalExecTime += execTime
	stats.avgCalculator.Add(execTime)

	if execTime < stats.minExecTime {
		stats.minExecTime = execTime
	}
	if execTime > stats.maxExecTime {
		stats.maxExecTime = execTime
	}

	// Update function statistics for callee
	t.updateFunctionStats(callee, execTime, now)

	// Update caller-callee mappings
	if t.callerToCallees[caller] == nil {
		t.callerToCallees[caller] = make(map[string]bool)
	}
	t.callerToCallees[caller][callee] = true

	if t.calleeToCamllers[callee] == nil {
		t.calleeToCamllers[callee] = make(map[string]bool)
	}
	t.calleeToCamllers[callee][caller] = true
}

// RecordCall records a call without execution time (uses 0)
func (t *CallGraphTracker) RecordCall(caller, callee string) {
	t.Record(caller, callee, 0)
}

// updateFunctionStats updates the statistics for a function (must be called with lock held)
func (t *CallGraphTracker) updateFunctionStats(functionName string, execTime time.Duration, timestamp time.Time) {
	stats, exists := t.functionStats[functionName]
	if !exists {
		stats = &functionInternalStats{
			name:          functionName,
			minExecTime:   execTime,
			maxExecTime:   execTime,
			avgCalculator: NewAveragingCalculator(t.config),
		}
		t.functionStats[functionName] = stats
	}

	stats.totalCalls++
	stats.totalExecTime += execTime
	stats.lastExecTime = execTime
	stats.lastCalledAt = timestamp
	stats.avgCalculator.Add(execTime)

	if execTime < stats.minExecTime || stats.minExecTime == 0 {
		stats.minExecTime = execTime
	}
	if execTime > stats.maxExecTime {
		stats.maxExecTime = execTime
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

	functions := t.getAllFunctionStatsInternal()

	totalCalls := 0
	for _, stats := range t.functionStats {
		totalCalls += stats.totalCalls
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

	return FunctionStats{
		Name:               stats.name,
		TotalCalls:         stats.totalCalls,
		TotalExecutionTime: stats.totalExecTime,
		AvgExecutionTime:   stats.avgCalculator.Average(),
		MinExecutionTime:   stats.minExecTime,
		MaxExecutionTime:   stats.maxExecTime,
		LastExecutionTime:  stats.lastExecTime,
		LastCalledAt:       stats.lastCalledAt,
	}, true
}

// GetAllFunctionStats returns statistics for all functions
func (t *CallGraphTracker) GetAllFunctionStats() map[string]FunctionStats {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	return t.getAllFunctionStatsInternal()
}

// getAllFunctionStatsInternal returns function stats (must be called with lock held)
func (t *CallGraphTracker) getAllFunctionStatsInternal() map[string]FunctionStats {
	result := make(map[string]FunctionStats, len(t.functionStats))
	for name, stats := range t.functionStats {
		result[name] = FunctionStats{
			Name:               stats.name,
			TotalCalls:         stats.totalCalls,
			TotalExecutionTime: stats.totalExecTime,
			AvgExecutionTime:   stats.avgCalculator.Average(),
			MinExecutionTime:   stats.minExecTime,
			MaxExecutionTime:   stats.maxExecTime,
			LastExecutionTime:  stats.lastExecTime,
			LastCalledAt:       stats.lastCalledAt,
		}
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
	if callerMap, ok := t.calleeToCamllers[functionName]; ok {
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
	t.functionStats = make(map[string]*functionInternalStats)
	t.callerToCallees = make(map[string]map[string]bool)
	t.calleeToCamllers = make(map[string]map[string]bool)
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
	t.functionStats = make(map[string]*functionInternalStats)
	t.callerToCallees = make(map[string]map[string]bool)
	t.calleeToCamllers = make(map[string]map[string]bool)
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
			avgCalculator: NewAveragingCalculator(t.config),
		}

		// Update caller-callee mappings
		if t.callerToCallees[edge.Caller] == nil {
			t.callerToCallees[edge.Caller] = make(map[string]bool)
		}
		t.callerToCallees[edge.Caller][edge.Callee] = true

		if t.calleeToCamllers[edge.Callee] == nil {
			t.calleeToCamllers[edge.Callee] = make(map[string]bool)
		}
		t.calleeToCamllers[edge.Callee][edge.Caller] = true
	}

	// Restore function stats
	for name, stats := range graph.Functions {
		t.functionStats[name] = &functionInternalStats{
			name:          stats.Name,
			totalCalls:    stats.TotalCalls,
			totalExecTime: stats.TotalExecutionTime,
			minExecTime:   stats.MinExecutionTime,
			maxExecTime:   stats.MaxExecutionTime,
			lastExecTime:  stats.LastExecutionTime,
			lastCalledAt:  stats.LastCalledAt,
			avgCalculator: NewAveragingCalculator(t.config),
		}
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

	// Mark as visited in current path
	pathVisited[current] = true

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
			t.findAllPaths(callee, newPath, pathVisited, paths)
		}
	}

	// Unmark when backtracking (allows the same node to be visited in different branches)
	pathVisited[current] = false
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

		if callers, ok := t.calleeToCamllers[current]; ok {
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
			functions[name] = FunctionStats{
				Name:               stats.name,
				TotalCalls:         stats.totalCalls,
				TotalExecutionTime: stats.totalExecTime,
				AvgExecutionTime:   stats.avgCalculator.Average(),
				MinExecutionTime:   stats.minExecTime,
				MaxExecutionTime:   stats.maxExecTime,
				LastExecutionTime:  stats.lastExecTime,
				LastCalledAt:       stats.lastCalledAt,
			}
			totalCalls += stats.totalCalls
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

// Verify that CallGraphTracker implements the interfaces
var _ Tracker = (*CallGraphTracker)(nil)
var _ Serializer = (*CallGraphTracker)(nil)
var _ PathAnalyzer = (*CallGraphTracker)(nil)
var _ FullTracker = (*CallGraphTracker)(nil)
