package callgraph

import (
	"time"
)

// AveragingMethod defines the method used for calculating execution time averages
type AveragingMethod int

const (
	// SimpleMovingAverage calculates the simple moving average over a window
	SimpleMovingAverage AveragingMethod = iota
	// ExponentialMovingAverage calculates the exponential moving average
	ExponentialMovingAverage
)

// Config holds the configuration for the CallGraphTracker
type Config struct {
	// AveragingMethod specifies which averaging method to use (SMA or EMA)
	AveragingMethod AveragingMethod

	// SMAWindowSize is the window size for Simple Moving Average
	// Only used when AveragingMethod is SimpleMovingAverage
	SMAWindowSize int

	// EMAAlpha is the smoothing factor for Exponential Moving Average (0 < alpha <= 1)
	// Higher values give more weight to recent observations
	// Only used when AveragingMethod is ExponentialMovingAverage
	EMAAlpha float64

	// MaxEdges is the maximum number of detailed edges to keep (0 = unlimited)
	MaxEdges int
}

// DefaultConfig returns the default configuration
func DefaultConfig() Config {
	return Config{
		AveragingMethod: SimpleMovingAverage,
		SMAWindowSize:   10,
		EMAAlpha:        0.3,
		MaxEdges:        10000,
	}
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
