package callgraph

import "time"

// Tracker defines the interface for tracking function call graphs
type Tracker interface {
	// Enabled returns whether the call graph tracking is enabled
	Enabled() bool

	// StartExecution marks the beginning of a function execution for a specific request
	// This should be called before invoking the function container
	StartExecution(functionName string, requestID string, timestamp time.Time)

	// RecordEdge records an edge in the call graph when caller invokes callee
	// It automatically calculates edge execution time from the execution context
	// caller is empty string for external calls (entry points)
	RecordEdge(caller, callee string, requestID string, timestamp time.Time)

	// EndExecution marks the end of a function execution for a specific request
	// It calculates and records function execution stats, then cleans up the execution context
	EndExecution(functionName string, requestID string, timestamp time.Time)

	// RecordColdStart records a cold start event for the given function
	// It is a shortcut to RecordScaleUp with cold=true
	RecordColdStart(functionName string, timestamp time.Time, executionTime time.Duration)

	// RecordScaleUp records a scale-up event for the given function
	RecordScaleUp(functionName string, timestamp time.Time, duration time.Duration, cold bool)

	// RecordScaleDown records a scale-down event for the given function
	RecordScaleDown(functionName string, timestamp time.Time, duration time.Duration)

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

	// Start starts any background processes related to the tracker
	Start()

	// Stop stops any background processes related to the tracker
	Stop()

	// EdgeCount returns the number of unique edges
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

	// GetEntryPoints returns all functions that are called from external sources
	GetEntryPoints() []string

	// GetLeafFunctions returns all functions that don't call any other functions
	GetLeafFunctions() []string

	// GetDownstreamFunctions returns all functions reachable from the given function
	GetDownstreamFunctions(functionName string) []string

	// GetUpstreamFunctions returns all functions that can reach the given function
	GetUpstreamFunctions(functionName string) []string
}

// Prewarmer defines the interface for prewarming prediction
type Prewarmer interface {
	// HasSufficientData checks if there is enough historical data to make
	// prewarming predictions for a specific function
	HasSufficientData(functionName string) bool

	// HasSufficientEdgeData checks if there is enough historical data for a specific edge
	HasSufficientEdgeData(caller, callee string) bool

	// GetPrewarmTargets returns a list of functions that should be prewarmed
	// when the given function starts execution
	GetPrewarmTargets(functionName string) []PrewarmTarget
}

// FullTracker combines all callgraph interfaces
type FullTracker interface {
	Tracker
	Serializer
	PathAnalyzer
	Prewarmer
}
