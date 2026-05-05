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

func (m AveragingMethod) String() string {
	switch m {
	case ExponentialMovingAverage:
		return "EMA"
	case SimpleMovingAverage:
		fallthrough
	default:
		return "SMA"
	}
}

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

	// Method selects the averaging strategy used for execution-time statistics.
	Method AveragingMethod

	// SMAConfig applies when Method is SimpleMovingAverage.
	SMAConfig *SMAConfig

	// EMAConfig applies when Method is ExponentialMovingAverage.
	EMAConfig *EMAConfig

	// ContextTTL is the time-to-live for execution contexts
	// Contexts older than this will be cleaned up to prevent memory leaks
	// Default is 5 minutes if not set
	ContextTTL time.Duration

	// ContextCleanupInterval is how often to run the cleanup goroutine
	// Default is 1 minute if not set
	ContextCleanupInterval time.Duration

	// Prewarm holds the prewarming configuration
	Prewarm *PrewarmConfig
}

type Option func(*CallGraphTracker)

// CallGraphTracker tracks function call relationships with execution time
type CallGraphTracker struct {
	config *Config

	logger *zap.Logger

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

	// Execution context tracking: requestID -> executionID -> context
	executionContexts map[string]map[string]executionContext

	// Track when each request context was last accessed (for TTL cleanup)
	contextLastAccess map[string]time.Time

	// When tracking started
	startTime time.Time

	// Cleanup goroutine control
	cleanupStop chan struct{}
	cleanupDone chan struct{}

	mutex sync.RWMutex
}

// executionContext holds information about a function's execution
type executionContext struct {
	functionName string
	startTime    time.Time
	endTime      time.Time
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

	// TotalScaleUps is the total number of scale-ups for this function
	TotalScaleUps int `json:"total_scale_ups"`

	// TotalPrewarms is the total number of proactive prewarms for this function
	TotalPrewarms int `json:"total_prewarms"`

	// TotalScaleDowns is the total number of scale-downs for this function
	TotalScaleDowns int `json:"total_scale_downs"`

	// TotalResets is the number of times this function's stats were reset (redeployments)
	TotalResets int `json:"total_resets"`

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

	// LastPrewarmAt is when the function was last proactively prewarmed
	LastPrewarmAt time.Time `json:"last_prewarm_at"`

	// LastPrewarmDuration is the duration of the last proactive prewarm
	LastPrewarmDuration time.Duration `json:"last_prewarm_duration_ns"`

	// AvgColdStartDuration is the calculated average cold start duration
	AvgColdStartDuration time.Duration `json:"avg_cold_start_duration_ns"`

	// LastResetAt is when the function stats were last reset (redeployment)
	LastResetAt time.Time `json:"last_reset_at,omitempty"`

	// avgExecCalculator is the internal calculator for moving averages (not serialized)
	avgExecCalculator AveragingCalculator `json:"-"`

	// avgScaleUpCalculator is the internal calculator for scale-up (also cold-start) averages (not serialized)
	avgScaleUpCalculator AveragingCalculator `json:"-"`
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

// PrewarmTarget represents a function that should be prewarmed
type PrewarmTarget struct {
	// FunctionName is the name of the function to prewarm
	FunctionName string `json:"function_name"`

	// LeadTime is the time before the function is expected to be called
	LeadTime time.Duration `json:"lead_time_ns"`
}

// PrewarmConfig holds configuration for prewarming decisions
type PrewarmConfig struct {
	// Enabled indicates whether prewarming is enabled
	Enabled bool
}

// DefaultPrewarmConfig returns the default prewarming configuration
func DefaultPrewarmConfig() *PrewarmConfig {
	return &PrewarmConfig{
		Enabled: false,
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
