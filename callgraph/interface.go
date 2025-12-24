package callgraph

import "time"

// Tracker defines the interface for tracking function call graphs
type Tracker interface {
	// RecordEdge records a call from caller to callee with the given execution time
	// caller is empty string for external calls (entry points)
	RecordEdge(caller, callee string, executionTime time.Duration)

	// RecordEdgeCall is a simplified version that records without execution time
	RecordEdgeCall(caller, callee string)

	// RecordFuncExec records the execution of a function with the given execution time
	RecordFuncExec(functionName string, timestamp time.Time, executionTime time.Duration)

	// RecordColdStart records a cold start event for the given function
	RecordColdStart(functionName string, timestamp time.Time, executionTime time.Duration)

	// GetCallGraph returns the complete call graph with aggregated data
	GetCallGraph() CallGraph

	// GetFunctionStats returns statistics for a specific function
	GetFunctionStats(functionName string) (FunctionStats, bool)

	// GetAllFunctionStats returns statistics for all functions
	GetAllFunctionStats() map[string]FunctionStats

	// GetCallees returns all functions that the given function calls
	GetCallees(functionName string) []string

	// GetCallers returns all functions that call the given function
	GetCallers(functionName string) []string

	// GetAverageExecutionTime returns the calculated average execution time for a function
	// Uses the configured averaging method (SMA or EMA)
	GetAverageExecutionTime(functionName string) int64

	// GetEdgeStats returns the aggregated stats for a specific caller->callee edge
	GetEdgeStats(caller, callee string) (AggregatedEdge, bool)

	// Clear clears all recorded data
	Clear()

	// EdgeCount returns the total number of recorded edges
	EdgeCount() int

	// FunctionCount returns the number of unique functions tracked
	FunctionCount() int
}

// Serializer defines the interface for serializing call graph data
type Serializer interface {
	// ToJSON returns the call graph as JSON bytes
	ToJSON() ([]byte, error)

	// FromJSON loads call graph data from JSON bytes
	FromJSON(data []byte) error
}

// PathAnalyzer defines the interface for analyzing call paths in DAG topologies
type PathAnalyzer interface {
	// GetCallPaths returns all unique call paths starting from external calls
	// Properly handles DAG topologies with branches
	GetCallPaths() []CallPath

	// GetPathsContaining returns all paths that contain the given function
	GetPathsContaining(functionName string) []CallPath

	// GetLongestPath returns the longest call path by number of functions
	GetLongestPath() CallPath

	// GetSlowestPath returns the call path with the highest total execution time
	GetSlowestPath() CallPath

	// GetEntryPoints returns all functions that are called from external sources
	GetEntryPoints() []string

	// GetLeafFunctions returns all functions that don't call any other functions
	GetLeafFunctions() []string

	// GetDownstreamFunctions returns all functions reachable from the given function
	GetDownstreamFunctions(functionName string) []string

	// GetUpstreamFunctions returns all functions that can reach the given function
	GetUpstreamFunctions(functionName string) []string

	// GetSubgraph returns a CallGraph containing only the specified functions and their edges
	GetSubgraph(functionNames []string) CallGraph
}

// FullTracker combines all callgraph interfaces
type FullTracker interface {
	Tracker
	Serializer
	PathAnalyzer
}
