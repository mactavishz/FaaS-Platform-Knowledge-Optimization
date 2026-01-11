package callgraph

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

// AveragingMethod defines the method used for calculating execution time averages
type AveragingMethod int

const (
	// SimpleMovingAverage calculates the simple moving average over a window
	SimpleMovingAverage AveragingMethod = iota + 1
	// ExponentialMovingAverage calculates the exponential moving average
	ExponentialMovingAverage
)

type SMAConfig struct {
	// WindowSize is the window size for Simple Moving Average
	// Only used when AveragingMethod is SimpleMovingAverage
	WindowSize int
}

type EMAConfig struct {
	// Alpha is the smoothing factor for Exponential Moving Average (0 < alpha <= 1)
	// Higher values give more weight to recent observations
	// Only used when AveragingMethod is ExponentialMovingAverage
	Alpha float64
}

// Config holds the configuration for the CallGraphTracker
type Config struct {
	// Enabled indicates whether call graph tracking is enabled
	Enabled bool

	// MaxEdges is the maximum number of detailed edges to keep (-1 = unlimited)
	MaxEdges int

	// ContextTTL is the time-to-live for execution contexts
	// Contexts older than this will be cleaned up to prevent memory leaks
	// Default is 5 minutes if not set
	ContextTTL time.Duration

	// ContextCleanupInterval is how often to run the cleanup goroutine
	// Default is 1 minute if not set
	ContextCleanupInterval time.Duration

	// Prewarm holds the prewarming configuration
	Prewarm PrewarmConfig
}

type Option func(*CallGraphTracker)

// CallGraphTracker tracks function call relationships with execution time
type CallGraphTracker struct {
	config *Config

	logger *zap.Logger

	// Detailed edges (recent calls only based on MaxEdges)
	edges []CallEdge

	averagingMethod AveragingMethod

	smaConfig *SMAConfig

	emaConfig *EMAConfig

	// Edge statistics: edgeKey -> stats
	edgeStats map[string]*edgeStats

	// Function statistics: functionName -> stats
	functionStats map[string]*FunctionStats

	// Caller to callees mapping for quick lookup
	callerToCallees map[string]map[string]bool

	// Callee to callers mapping for quick lookup
	calleeToCallers map[string]map[string]bool

	// Execution context tracking: requestID -> functionName -> startTime
	executionContexts map[string]map[string]time.Time

	// Track when each request context was last accessed (for TTL cleanup)
	contextLastAccess map[string]time.Time

	// When tracking started
	startTime time.Time

	// Cleanup goroutine control
	cleanupStop chan struct{}
	cleanupDone chan struct{}

	mutex sync.RWMutex
}

// CallEdge represents a single call from one function to another
type CallEdge struct {
	// Caller is the function name that initiated the call, empty string for external calls
	Caller string `json:"caller"`

	// Callee is the function name that was called
	Callee string `json:"callee"`

	// ExecutionTime is the duration of the callee function execution
	ExecutionTime time.Duration `json:"execution_time_ns"`

	// Timestamp is when the call occurred
	Timestamp time.Time `json:"timestamp"`
}

// AggregatedEdge represents aggregated call data between two functions
type AggregatedEdge struct {
	// Caller is the function name that initiated the call
	Caller string `json:"caller"`

	// Callee is the function name that was called
	Callee string `json:"callee"`

	// Count is the number of times this call has been made
	Count int `json:"count"`

	// TotalExecutionTime is the sum of all execution times for this edge
	TotalExecutionTime time.Duration `json:"total_execution_time_ns"`

	// AvgExecutionTime is the calculated average execution time
	AvgExecutionTime time.Duration `json:"avg_execution_time_ns"`

	// MinExecutionTime is the minimum execution time observed
	MinExecutionTime time.Duration `json:"min_execution_time_ns"`

	// MaxExecutionTime is the maximum execution time observed
	MaxExecutionTime time.Duration `json:"max_execution_time_ns"`
}

// FunctionStats holds execution statistics for a single function
type FunctionStats struct {
	// Name is the function name
	Name string `json:"name"`

	// TotalCalls is the total number of times this function was called
	TotalCalls int `json:"total_calls"`

	// TotalColdStarts is the total number of cold starts for this function
	TotalColdStarts int `json:"total_cold_starts"`

	// TotalExecutionTime is the sum of all execution times
	TotalExecutionTime time.Duration `json:"total_execution_time_ns"`

	// AvgExecutionTime is the calculated average execution time
	AvgExecutionTime time.Duration `json:"avg_execution_time_ns"`

	// MinExecutionTime is the minimum execution time observed
	MinExecutionTime time.Duration `json:"min_execution_time_ns"`

	// MaxExecutionTime is the maximum execution time observed
	MaxExecutionTime time.Duration `json:"max_execution_time_ns"`

	// LastExecutionTime is the most recent execution time
	LastExecutionTime time.Duration `json:"last_execution_time_ns"`

	// LastCalledAt is when the function was last called
	LastCalledAt time.Time `json:"last_called_at"`

	// LastColdStartAt is when the function was last cold started
	LastColdStartAt time.Time `json:"last_cold_start_at"`

	// LastColdStartDuration is the duration of the last cold start
	LastColdStartDuration time.Duration `json:"last_cold_start_duration_ns"`

	// AvgColdStartDuration is the calculated average cold start duration
	AvgColdStartDuration time.Duration `json:"avg_cold_start_duration_ns"`

	// avgCalculator is the internal calculator for moving averages (not serialized)
	avgCalculator AveragingCalculator `json:"-"`

	// avgColdStartCalculator is the internal calculator for cold start averages (not serialized)
	avgColdStartCalculator AveragingCalculator `json:"-"`
}

// CallGraph represents the complete call graph structure
type CallGraph struct {
	// Edges is the list of all aggregated edges
	Edges []AggregatedEdge `json:"edges"`

	// Functions is the map of function names to their stats
	Functions map[string]FunctionStats `json:"functions"`

	// TotalCalls is the total number of calls recorded
	TotalCalls int `json:"total_calls"`

	// RecordedSince is when the tracking started
	RecordedSince time.Time `json:"recorded_since"`
}

// CallPath represents a sequence of function calls forming a workflow path
type CallPath struct {
	// Path is the ordered list of function names in the call path
	Path []string `json:"path"`

	// TotalExecutionTime is the sum of execution times for all functions in the path
	TotalExecutionTime time.Duration `json:"total_execution_time_ns"`

	// Count is how many times this exact path has been observed
	Count int `json:"count"`
}

// PrewarmTarget represents a function that should be prewarmed
type PrewarmTarget struct {
	// FunctionName is the name of the function to prewarm
	FunctionName string `json:"function_name"`

	// LeadTime is the expected time before this function is called
	// This is based on the average edge time from the caller
	LeadTime time.Duration `json:"lead_time_ns"`

	// Confidence is a value between 0 and 1 indicating how confident we are
	// that this function will be called, based on historical call frequency
	Confidence float64 `json:"confidence"`
}

// PrewarmConfig holds configuration for prewarming decisions
type PrewarmConfig struct {
	// Enabled indicates whether prewarming is enabled
	Enabled bool

	// MinSamples is the minimum number of edge samples required before
	// making prewarming decisions (default: 3)
	MinSamples int

	// Threshold is the ratio of edge time to cold start time that triggers prewarming
	// Prewarm if: avgEdgeTime >= avgColdStartTime * Threshold
	// Default: 0.8 (prewarm if we have at least 80% of cold start time available)
	Threshold float64
}

// DefaultPrewarmConfig returns the default prewarming configuration
func DefaultPrewarmConfig() PrewarmConfig {
	return PrewarmConfig{
		Enabled:    true,
		MinSamples: 3,
		Threshold:  0.8,
	}
}

// edgeStats holds internal statistics for an edge
// the execution time means the time taken for the caller to call the callee, the caller might wait for the callee to finish
// or the caller might still has some work to do after calling the callee.
type edgeStats struct {
	caller        string
	callee        string
	count         int
	totalExecTime time.Duration
	minExecTime   time.Duration
	maxExecTime   time.Duration
	avgCalculator AveragingCalculator
}

// edgeKey creates a unique key for a caller->callee edge
func edgeKey(caller, callee string) string {
	return caller + "->" + callee
}
