# CallGraph Package

A platform-agnostic call graph tracking package for FaaS platforms. This package is designed to track function call relationships and execution times within serverless workflows, supporting both faasd and tinyFaaS platforms.

## Features

- **DAG Support**: Full support for Directed Acyclic Graph topologies with branches (not just linear chains)
- **Call Graph Tracking**: Track function-to-function call relationships
- **Execution Time Recording**: Record and analyze execution times for each call
- **Configurable Averaging**: Support for both Simple Moving Average (SMA) and Exponential Moving Average (EMA)
- **Path Analysis**: Analyze complete call paths through branching workflows
- **Graph Traversal**: Find upstream/downstream functions, entry points, and leaf nodes
- **Thread-Safe**: All operations are safe for concurrent access
- **JSON Serialization**: Easy persistence and data exchange

## Supported Topologies

The package supports any acyclic call graph topology:

```
Linear:     A -> B -> C -> D

Branching:  A -> B -> D
            A -> C -> D

Diamond:    A -> B -> D
            A -> C -> D
            (B and C both call D)

Complex:    A -> B -> D -> F
            A -> C -> E -> F
            A -> C -> E -> G
```

## Installation

```go
import "github.com/mactavishz/FaaS-Platform-Knowledge-Optimization/pkg/callgraph"
```

## Quick Start

```go
package main

import (
    "fmt"
    "time"
    
    "github.com/mactavishz/FaaS-Platform-Knowledge-Optimization/pkg/callgraph"
)

func main() {
    // Create a new tracker with default configuration (SMA with window size 10)
    tracker := callgraph.New()

    // Or create with custom configuration
    config := callgraph.Config{
        AveragingMethod: callgraph.ExponentialMovingAverage,
        EMAAlpha:        0.3,
        MaxEdges:        10000,
    }
    tracker = callgraph.NewWithConfig(config)

    // Record calls (caller, callee, execution time in nanoseconds)
    // Use empty string for external calls (entry points)
    tracker.Record("", "functionA", int64(100*time.Millisecond))
    tracker.Record("functionA", "functionB", int64(50*time.Millisecond))
    tracker.Record("functionA", "functionC", int64(75*time.Millisecond))
    tracker.Record("functionB", "functionD", int64(25*time.Millisecond))

    // Get function statistics
    stats, ok := tracker.GetFunctionStats("functionA")
    if ok {
        fmt.Printf("Function A - Calls: %d, Avg Time: %v\n", 
            stats.TotalCalls, stats.AvgExecutionTime)
    }

    // Get edge statistics
    edgeStats, ok := tracker.GetEdgeStats("functionA", "functionB")
    if ok {
        fmt.Printf("A->B calls: %d, Avg Time: %v\n",
            edgeStats.Count, edgeStats.AvgExecutionTime)
    }

    // Get complete call graph
    graph := tracker.GetCallGraph()
    fmt.Printf("Total functions: %d, Total edges: %d\n",
        len(graph.Functions), len(graph.Edges))

    // Analyze call paths
    paths := tracker.GetCallPaths()
    for _, path := range paths {
        fmt.Printf("Path: %v, Total Time: %v\n",
            path.Path, path.TotalExecutionTime)
    }

    // Export to JSON
    jsonData, _ := tracker.ToJSON()
    fmt.Println(string(jsonData))
}
```

## Configuration

### AveragingMethod

- `SimpleMovingAverage` (default): Calculates average over a sliding window
- `ExponentialMovingAverage`: Gives more weight to recent observations

### Config Options

```go
type Config struct {
    // AveragingMethod: SMA or EMA
    AveragingMethod AveragingMethod

    // SMAWindowSize: Window size for SMA (default: 10)
    SMAWindowSize int

    // EMAAlpha: Smoothing factor for EMA, 0 < alpha <= 1 (default: 0.3)
    // Higher values = more weight on recent data
    EMAAlpha float64

    // MaxEdges: Maximum detailed edges to keep, 0 = unlimited (default: 10000)
    MaxEdges int
}
```

## Interfaces

The package provides clean interfaces for flexibility:

```go
// Tracker - Core tracking functionality
type Tracker interface {
    Record(caller, callee string, executionTime int64)
    RecordCall(caller, callee string)
    GetCallGraph() CallGraph
    GetFunctionStats(functionName string) (FunctionStats, bool)
    GetAllFunctionStats() map[string]FunctionStats
    GetCallees(functionName string) []string
    GetCallers(functionName string) []string
    GetAverageExecutionTime(functionName string) int64
    GetEdgeStats(caller, callee string) (AggregatedEdge, bool)
    Clear()
    EdgeCount() int
    FunctionCount() int
}

// Serializer - JSON import/export
type Serializer interface {
    ToJSON() ([]byte, error)
    FromJSON(data []byte) error
}

// PathAnalyzer - DAG path analysis (supports branching topologies)
type PathAnalyzer interface {
    GetCallPaths() []CallPath              // All paths from entry points to leaves
    GetPathsContaining(functionName string) []CallPath
    GetLongestPath() CallPath
    GetSlowestPath() CallPath
    GetEntryPoints() []string              // Functions called externally
    GetLeafFunctions() []string            // Functions that don't call others
    GetDownstreamFunctions(fn string) []string  // All reachable functions
    GetUpstreamFunctions(fn string) []string    // All functions that can reach fn
    GetSubgraph(functionNames []string) CallGraph
}

// FullTracker - Combined interface
type FullTracker interface {
    Tracker
    Serializer
    PathAnalyzer
}
```

## Data Types

### CallEdge
Represents a single call event:
```go
type CallEdge struct {
    Caller        string        // Source function ("" for external)
    Callee        string        // Target function
    ExecutionTime time.Duration // Execution duration
    Timestamp     time.Time     // When the call occurred
}
```

### FunctionStats
Statistics for a single function:
```go
type FunctionStats struct {
    Name               string
    TotalCalls         int
    TotalExecutionTime time.Duration
    AvgExecutionTime   time.Duration
    MinExecutionTime   time.Duration
    MaxExecutionTime   time.Duration
    LastExecutionTime  time.Duration
    LastCalledAt       time.Time
}
```

### AggregatedEdge
Statistics for a caller->callee edge:
```go
type AggregatedEdge struct {
    Caller             string
    Callee             string
    Count              int
    TotalExecutionTime time.Duration
    AvgExecutionTime   time.Duration
    MinExecutionTime   time.Duration
    MaxExecutionTime   time.Duration
}
```

### CallPath
A complete execution path through the workflow:
```go
type CallPath struct {
    Path               []string      // Ordered function names
    TotalExecutionTime time.Duration // Sum of execution times
    Count              int           // How many times observed
}
```

## Usage with faasd/tinyFaaS

### Recording Calls

In your platform's reverse proxy or function handler:

```go
// When a function is invoked
start := time.Now()

// Execute the function...

executionTime := time.Since(start)

// Record the call
// caller is the X-Caller header or "" for external calls
tracker.Record(caller, functionName, executionTime.Nanoseconds())
```

### Propagating Call Context

When a function calls another function, pass the caller information:
```go
// In the function handler, set the X-Caller header
req.Header.Set("X-Caller", currentFunction)
```

### Exporting Metrics

```go
// Expose via HTTP endpoint
http.HandleFunc("/callgraph", func(w http.ResponseWriter, r *http.Request) {
    data, err := tracker.ToJSON()
    if err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    w.Write(data)
})
```

## Thread Safety

All methods on `CallGraphTracker` are thread-safe and can be called concurrently from multiple goroutines.

## License

MIT License
