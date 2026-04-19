package callgraph

import (
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

var testRequestCounter int64

// Test helper: simulates a complete edge recording with full API flow
// This records an edge from caller to callee with the specified edge time,
// and also records the callee's execution time (NOT the caller's)
func simulateEdge(t *CallGraphTracker, caller, callee string, edgeTime, execTime time.Duration) string {
	counter := atomic.AddInt64(&testRequestCounter, 1)
	requestID := fmt.Sprintf("test-%d-%d", time.Now().UnixNano(), counter)
	callerExecutionID := fmt.Sprintf("exec-caller-%d-%d", time.Now().UnixNano(), counter)
	calleeExecutionID := fmt.Sprintf("exec-callee-%d-%d", time.Now().UnixNano(), counter)
	now := time.Now()

	// If caller exists, start its execution first (needed for edge time calculation)
	if caller != "" {
		t.StartExecution(caller, requestID, callerExecutionID, now)
	}

	// Record the edge (caller -> callee)
	callTime := now.Add(edgeTime)
	t.RecordEdge(caller, callee, requestID, callerExecutionID, callTime)

	// Start and end callee execution (this records callee's function stats)
	t.StartExecution(callee, requestID, calleeExecutionID, callTime)
	endTime := callTime.Add(execTime)
	t.EndExecution(callee, requestID, calleeExecutionID, endTime)

	// Clean up caller's context without recording stats (caller is managed separately)
	// We manually clean up instead of calling EndExecution to avoid recording caller stats
	if caller != "" {
		t.mutex.Lock()
		if requestCtx, ok := t.executionContexts[requestID]; ok {
			delete(requestCtx, callerExecutionID)
			if len(requestCtx) == 0 {
				delete(t.executionContexts, requestID)
				delete(t.contextLastAccess, requestID)
			}
		}
		t.mutex.Unlock()
	}

	return requestID
}

// Test helper: simulates only function execution (no edge)
func simulateFuncExec(t *CallGraphTracker, functionName string, execTime time.Duration) string {
	counter := atomic.AddInt64(&testRequestCounter, 1)
	requestID := fmt.Sprintf("test-%d-%d", time.Now().UnixNano(), counter)
	executionID := fmt.Sprintf("exec-%d-%d", time.Now().UnixNano(), counter)
	now := time.Now()

	t.StartExecution(functionName, requestID, executionID, now)
	endTime := now.Add(execTime)
	t.EndExecution(functionName, requestID, executionID, endTime)

	return requestID
}

func TestNewTracker(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()
	assert.Equal(t, 0, tracker.EdgeCount())
	assert.Equal(t, 0, tracker.FunctionCount())
	assert.Equal(t, SimpleMovingAverage, tracker.averagingMethod)
}

func TestNewTrackerWithConfig(t *testing.T) {
	config := &Config{
		Enabled:                true,
		ContextTTL:             1,
		ContextCleanupInterval: 1,
		Prewarm: &PrewarmConfig{
			Enabled: true,
		},
	}
	tracker := New(
		WithConfig(config),
		WithLogger(zap.NewNop()),
	)
	tracker.Start()
	defer tracker.Stop()
	assert.Equal(t, SimpleMovingAverage, tracker.averagingMethod)
}

func TestNewTrackerWithConfigEMA(t *testing.T) {
	config := &Config{
		Enabled:                true,
		Method:                 ExponentialMovingAverage,
		EMAConfig:              &EMAConfig{Alpha: 0.42},
		ContextTTL:             1,
		ContextCleanupInterval: 1,
		Prewarm: &PrewarmConfig{
			Enabled: true,
		},
	}

	tracker := New(
		WithConfig(config),
		WithLogger(zap.NewNop()),
	)
	tracker.Start()
	defer tracker.Stop()

	assert.Equal(t, ExponentialMovingAverage, tracker.averagingMethod)
	require.NotNil(t, tracker.emaConfig)
	assert.Equal(t, 0.42, tracker.emaConfig.Alpha)
	assert.Nil(t, tracker.smaConfig)
}

func TestNewTrackerWithConfigInvalidMethodFallsBackToSMA(t *testing.T) {
	config := &Config{
		Enabled:                true,
		Method:                 AveragingMethod(99),
		ContextTTL:             1,
		ContextCleanupInterval: 1,
		Prewarm: &PrewarmConfig{
			Enabled: true,
		},
	}

	tracker := New(
		WithConfig(config),
		WithLogger(zap.NewNop()),
	)
	tracker.Start()
	defer tracker.Stop()

	assert.Equal(t, SimpleMovingAverage, tracker.averagingMethod)
	require.NotNil(t, tracker.smaConfig)
	assert.Equal(t, DEFAULT_SMA_WINDOW_SIZE, tracker.smaConfig.WindowSize)
	assert.Nil(t, tracker.emaConfig)
}

func TestRecord(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	// Record a call edge and function execution using full flow
	simulateEdge(tracker, "funcA", "funcB", 100*time.Millisecond, 50*time.Millisecond)

	// Check edge count
	assert.Equal(t, 1, tracker.EdgeCount())

	// Check function count (funcB has execution stats)
	assert.Equal(t, 1, tracker.FunctionCount())

	// Check callees
	callees := tracker.GetCallees("funcA")
	assert.Equal(t, []string{"funcB"}, callees)

	// Check callers
	callers := tracker.GetCallers("funcB")
	assert.Equal(t, []string{"funcA"}, callers)
}

func TestRecordExternalCall(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	// Record an external call (empty caller)
	simulateEdge(tracker, "", "funcA", 0, 50*time.Millisecond)

	// Check that external calls are tracked
	callees := tracker.GetCallees("")
	assert.Equal(t, []string{"funcA"}, callees)
}

func TestGetFunctionStats(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	// Record multiple function executions
	simulateFuncExec(tracker, "funcA", 100*time.Millisecond)
	simulateFuncExec(tracker, "funcA", 200*time.Millisecond)
	simulateFuncExec(tracker, "funcA", 150*time.Millisecond)

	stats, ok := tracker.GetFunctionStats("funcA")
	require.True(t, ok, "function stats not found")

	assert.Equal(t, 3, int(stats.TotalCalls))
	assert.Equal(t, 450*time.Millisecond, stats.TotalExecutionTime)
	assert.Equal(t, 100*time.Millisecond, stats.MinExecutionTime)
	assert.Equal(t, 200*time.Millisecond, stats.MaxExecutionTime)
}

func TestGetEdgeStats(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	simulateEdge(tracker, "funcA", "funcB", 100*time.Millisecond, 50*time.Millisecond)
	simulateEdge(tracker, "funcA", "funcB", 200*time.Millisecond, 50*time.Millisecond)

	stats, ok := tracker.GetEdgeStats("funcA", "funcB")
	require.True(t, ok, "edge stats not found")

	assert.Equal(t, 2, int(stats.Count))
	assert.Equal(t, 300*time.Millisecond, stats.TotalExecutionTime)
}

func TestGetCallGraph(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	simulateEdge(tracker, "", "funcA", 0, 50*time.Millisecond)
	simulateEdge(tracker, "funcA", "funcB", 100*time.Millisecond, 100*time.Millisecond)
	simulateEdge(tracker, "funcA", "funcC", 150*time.Millisecond, 150*time.Millisecond)

	graph := tracker.GetCallGraph()

	assert.Len(t, graph.Edges, 3)
	assert.Len(t, graph.Functions, 3)
	assert.Equal(t, 3, int(graph.TotalCalls))
}

func TestClear(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	simulateEdge(tracker, "funcA", "funcB", 100*time.Millisecond, 50*time.Millisecond)
	simulateEdge(tracker, "funcB", "funcC", 200*time.Millisecond, 50*time.Millisecond)

	tracker.Clear()

	assert.Equal(t, 0, tracker.EdgeCount())
	assert.Equal(t, 0, tracker.FunctionCount())
}

func TestToJSON(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	simulateEdge(tracker, "", "funcA", 0, 100*time.Millisecond)
	simulateEdge(tracker, "funcA", "funcB", 200*time.Millisecond, 50*time.Millisecond)

	data, err := tracker.ToJSON()
	require.NoError(t, err)

	var graph CallGraph
	err = json.Unmarshal(data, &graph)
	require.NoError(t, err)

	assert.Len(t, graph.Edges, 2)
}

func TestFromJSON(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	simulateEdge(tracker, "", "funcA", 0, 100*time.Millisecond)
	simulateEdge(tracker, "funcA", "funcB", 200*time.Millisecond, 200*time.Millisecond)

	data, err := tracker.ToJSON()
	require.NoError(t, err)

	// Create new tracker and load from JSON
	newTracker := New(WithLogger(zap.NewNop()))
	newTracker.Start()
	defer newTracker.Stop()
	err = newTracker.FromJSON(data)
	require.NoError(t, err)

	// Verify data was loaded
	assert.Equal(t, 2, newTracker.EdgeCount())
	assert.Equal(t, 2, newTracker.FunctionCount())
}

func TestGetCallPaths(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	// Create a workflow: external -> A -> B -> C
	simulateEdge(tracker, "", "funcA", 0, 100*time.Millisecond)
	simulateEdge(tracker, "funcA", "funcB", 200*time.Millisecond, 100*time.Millisecond)
	simulateEdge(tracker, "funcB", "funcC", 150*time.Millisecond, 50*time.Millisecond)

	paths := tracker.GetCallPaths()

	require.Len(t, paths, 1)
	assert.Len(t, paths[0].Path, 3)
	assert.Equal(t, []string{"funcA", "funcB", "funcC"}, paths[0].Path)
}

func TestGetAverageExecutionTime(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	simulateFuncExec(tracker, "funcA", 100*time.Millisecond)
	simulateFuncExec(tracker, "funcA", 200*time.Millisecond)
	simulateFuncExec(tracker, "funcA", 300*time.Millisecond)

	avg := tracker.GetAverageExecutionTime("funcA")

	// SMA with 3 samples: (100+200+300)/3 = 200ms
	expected := int64(200 * time.Millisecond)
	assert.Equal(t, expected, avg)
}

func TestConcurrentAccess(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()
	done := make(chan bool)

	// Concurrent writers
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				simulateEdge(tracker, "funcA", "funcB", time.Millisecond, time.Millisecond)
			}
			done <- true
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = tracker.GetCallGraph()
				_, _ = tracker.GetFunctionStats("funcA")
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 15; i++ {
		<-done
	}

	// Should have recorded 1000 function executions (10 writers * 100 each)
	stats, ok := tracker.GetFunctionStats("funcB")
	require.True(t, ok, "function stats not found")
	assert.Equal(t, 1000, int(stats.TotalCalls))
}

func TestInterfaceCompliance(t *testing.T) {
	// Verify that CallGraphTracker implements all interfaces
	var _ Tracker = (*CallGraphTracker)(nil)
	var _ Prewarmer = (*CallGraphTracker)(nil)
	var _ Serializer = (*CallGraphTracker)(nil)
	var _ PathAnalyzer = (*CallGraphTracker)(nil)
	var _ FullTracker = (*CallGraphTracker)(nil)
}

func TestPrewarmEnabledAccessor(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	assert.False(t, tracker.PrewarmEnabled())

	config := DefaultConfig()
	config.Prewarm.Enabled = true
	tracker = New(WithConfig(config), WithLogger(zap.NewNop()))
	assert.True(t, tracker.PrewarmEnabled())
}

// TestDAGWithBranches tests a DAG topology where one function calls multiple functions
// Graph: external -> A -> B
//
//	A -> C
func TestDAGWithBranches(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	// A calls both B and C (branching)
	simulateEdge(tracker, "", "A", 0, 100*time.Millisecond)
	simulateEdge(tracker, "A", "B", 50*time.Millisecond, 50*time.Millisecond)
	simulateEdge(tracker, "A", "C", 75*time.Millisecond, 50*time.Millisecond)

	paths := tracker.GetCallPaths()

	// Should have 2 paths: A->B and A->C
	assert.Len(t, paths, 2, "expected 2 paths for branching DAG")

	// Check that both paths exist
	pathStrs := make([]string, len(paths))
	for i, p := range paths {
		pathStrs[i] = ""
		for _, fn := range p.Path {
			pathStrs[i] += fn + "->"
		}
	}
	t.Logf("Paths found: %v", pathStrs)
}

// TestDAGWithDiamond tests a diamond-shaped DAG
// Graph: external -> A -> B -> D
//
//	A -> C -> D
func TestDAGWithDiamond(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	// Create diamond: A -> B -> D and A -> C -> D
	simulateEdge(tracker, "", "A", 0, 100*time.Millisecond)
	simulateEdge(tracker, "A", "B", 50*time.Millisecond, 50*time.Millisecond)
	simulateEdge(tracker, "A", "C", 75*time.Millisecond, 50*time.Millisecond)
	simulateEdge(tracker, "B", "D", 25*time.Millisecond, 25*time.Millisecond)
	simulateEdge(tracker, "C", "D", 30*time.Millisecond, 25*time.Millisecond)

	paths := tracker.GetCallPaths()

	// Should have 2 paths: A->B->D and A->C->D
	assert.Len(t, paths, 2, "expected 2 paths for diamond DAG")

	// Both paths should end with D
	for _, path := range paths {
		assert.Len(t, path.Path, 3)
		assert.Equal(t, "D", path.Path[len(path.Path)-1])
	}
}

// TestDAGWithMultipleEntryPoints tests a DAG with multiple entry points
func TestDAGWithMultipleEntryPoints(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	// Two entry points: X and Y, both eventually reach Z
	simulateEdge(tracker, "", "X", 0, 100*time.Millisecond)
	simulateEdge(tracker, "", "Y", 0, 100*time.Millisecond)
	simulateEdge(tracker, "X", "Z", 50*time.Millisecond, 50*time.Millisecond)
	simulateEdge(tracker, "Y", "Z", 50*time.Millisecond, 50*time.Millisecond)

	paths := tracker.GetCallPaths()

	// Should have 2 paths: X->Z and Y->Z
	assert.Len(t, paths, 2, "expected 2 paths for multiple entry points")

	entryPoints := tracker.GetEntryPoints()
	assert.Len(t, entryPoints, 2)
}

// TestDAGComplexTopology tests a more complex DAG
// Graph:     A -> B -> D -> F
//
//	A -> C -> E -> F
//	A -> C -> E -> G
func TestDAGComplexTopology(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	simulateEdge(tracker, "", "A", 0, 100*time.Millisecond)
	simulateEdge(tracker, "A", "B", 50*time.Millisecond, 50*time.Millisecond)
	simulateEdge(tracker, "A", "C", 60*time.Millisecond, 60*time.Millisecond)
	simulateEdge(tracker, "B", "D", 40*time.Millisecond, 40*time.Millisecond)
	simulateEdge(tracker, "C", "E", 45*time.Millisecond, 45*time.Millisecond)
	simulateEdge(tracker, "D", "F", 30*time.Millisecond, 30*time.Millisecond)
	simulateEdge(tracker, "E", "F", 35*time.Millisecond, 30*time.Millisecond)
	simulateEdge(tracker, "E", "G", 25*time.Millisecond, 25*time.Millisecond)

	paths := tracker.GetCallPaths()

	// Should have 3 paths:
	// A->B->D->F
	// A->C->E->F
	// A->C->E->G
	if !assert.Len(t, paths, 3, "expected 3 paths for complex DAG") {
		for i, p := range paths {
			t.Logf("Path %d: %v", i, p.Path)
		}
	}

	// Check leaf functions
	leaves := tracker.GetLeafFunctions()
	assert.Len(t, leaves, 2, "expected 2 leaf functions (F and G)")
}

func TestGetEntryPoints(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	simulateEdge(tracker, "", "A", 0, 100*time.Millisecond)
	simulateEdge(tracker, "", "B", 0, 100*time.Millisecond)
	simulateEdge(tracker, "A", "C", 50*time.Millisecond, 50*time.Millisecond)
	simulateEdge(tracker, "B", "C", 50*time.Millisecond, 50*time.Millisecond)

	entryPoints := tracker.GetEntryPoints()

	assert.Len(t, entryPoints, 2)
}

func TestGetLeafFunctions(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	simulateEdge(tracker, "", "A", 0, 100*time.Millisecond)
	simulateEdge(tracker, "A", "B", 50*time.Millisecond, 50*time.Millisecond)
	simulateEdge(tracker, "A", "C", 75*time.Millisecond, 75*time.Millisecond)
	// B and C are leaves (they don't call anything)

	leaves := tracker.GetLeafFunctions()

	assert.Len(t, leaves, 2)
}

func TestGetDownstreamFunctions(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	// A -> B -> D
	// A -> C -> D
	simulateEdge(tracker, "", "A", 0, 100*time.Millisecond)
	simulateEdge(tracker, "A", "B", 50*time.Millisecond, 50*time.Millisecond)
	simulateEdge(tracker, "A", "C", 75*time.Millisecond, 50*time.Millisecond)
	simulateEdge(tracker, "B", "D", 25*time.Millisecond, 25*time.Millisecond)
	simulateEdge(tracker, "C", "D", 30*time.Millisecond, 25*time.Millisecond)

	downstream := tracker.GetDownstreamFunctions("A")

	// A can reach B, C, D
	assert.Len(t, downstream, 3)

	downstreamFromB := tracker.GetDownstreamFunctions("B")
	assert.Equal(t, []string{"D"}, downstreamFromB)
}

func TestGetUpstreamFunctions(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	// A -> B -> D
	// A -> C -> D
	simulateEdge(tracker, "", "A", 0, 100*time.Millisecond)
	simulateEdge(tracker, "A", "B", 50*time.Millisecond, 50*time.Millisecond)
	simulateEdge(tracker, "A", "C", 75*time.Millisecond, 50*time.Millisecond)
	simulateEdge(tracker, "B", "D", 25*time.Millisecond, 25*time.Millisecond)
	simulateEdge(tracker, "C", "D", 30*time.Millisecond, 25*time.Millisecond)

	upstream := tracker.GetUpstreamFunctions("D")

	// D can be reached from A, B, C
	assert.Len(t, upstream, 3)

	upstreamToA := tracker.GetUpstreamFunctions("A")
	assert.Empty(t, upstreamToA)
}

// TestSelfLoopPrevention tests that self-loops are prevented
func TestSelfLoopPrevention(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	requestID := "test-selfloop"
	now := time.Now()

	// Start execution of funcA
	tracker.StartExecution("funcA", requestID, "exec-1", now)

	// Try to record a self-loop
	tracker.RecordEdge("funcA", "funcA", requestID, "exec-1", now.Add(100*time.Millisecond))

	// Should have no edges recorded (self-loop rejected)
	assert.Equal(t, 0, tracker.EdgeCount())

	// External call to self should be recorded (not a self-loop since caller is empty)
	simulateEdge(tracker, "", "funcA", 0, 100*time.Millisecond)
	assert.Equal(t, 1, tracker.EdgeCount())
}

// TestEmptyCalleeValidation tests that empty callee is rejected
func TestEmptyCalleeValidation(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	requestID := "test-empty-callee"
	now := time.Now()

	// Start execution
	tracker.StartExecution("funcA", requestID, "exec-1", now)

	// Try to record with empty callee
	tracker.RecordEdge("funcA", "", requestID, "exec-1", now.Add(100*time.Millisecond))

	// Should have no edges recorded
	assert.Equal(t, 0, tracker.EdgeCount())

	// Empty caller is valid (external call)
	simulateEdge(tracker, "", "funcA", 0, 100*time.Millisecond)
	assert.Equal(t, 1, tracker.EdgeCount())
}

// TestPathTraversalWithPotentialCycle tests the fixed path traversal
func TestPathTraversalWithPotentialCycle(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	// Create a complex branching structure
	// Entry -> A -> B -> D
	//       -> A -> C -> D
	// Both B and C call D, but D should appear in both paths
	simulateEdge(tracker, "", "A", 0, 100*time.Millisecond)
	simulateEdge(tracker, "A", "B", 50*time.Millisecond, 50*time.Millisecond)
	simulateEdge(tracker, "A", "C", 50*time.Millisecond, 50*time.Millisecond)
	simulateEdge(tracker, "B", "D", 25*time.Millisecond, 25*time.Millisecond)
	simulateEdge(tracker, "C", "D", 25*time.Millisecond, 25*time.Millisecond)

	paths := tracker.GetCallPaths()

	// Should find 2 distinct paths
	if !assert.Len(t, paths, 2, "expected 2 paths") {
		for i, p := range paths {
			t.Logf("Path %d: %v", i, p.Path)
		}
	}

	// Both paths should contain D
	dCount := 0
	for _, path := range paths {
		for _, fn := range path.Path {
			if fn == "D" {
				dCount++
				break
			}
		}
	}
	assert.Equal(t, 2, dCount, "expected D to appear in 2 paths")
}

// TestMultiLevelBranching tests complex multi-level branching
func TestMultiLevelBranching(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	// Create a tree structure:
	//       A
	//      / \
	//     B   C
	//    / \ / \
	//   D  E F  G
	simulateEdge(tracker, "", "A", 0, 100*time.Millisecond)
	simulateEdge(tracker, "A", "B", 50*time.Millisecond, 50*time.Millisecond)
	simulateEdge(tracker, "A", "C", 50*time.Millisecond, 50*time.Millisecond)
	simulateEdge(tracker, "B", "D", 25*time.Millisecond, 25*time.Millisecond)
	simulateEdge(tracker, "B", "E", 25*time.Millisecond, 25*time.Millisecond)
	simulateEdge(tracker, "C", "F", 25*time.Millisecond, 25*time.Millisecond)
	simulateEdge(tracker, "C", "G", 25*time.Millisecond, 25*time.Millisecond)

	paths := tracker.GetCallPaths()

	// Should have 4 paths: A->B->D, A->B->E, A->C->F, A->C->G
	if !assert.Len(t, paths, 4, "expected 4 paths") {
		for i, p := range paths {
			t.Logf("Path %d: %v", i, p.Path)
		}
	}

	// All paths should start with A and have length 3
	for i, path := range paths {
		assert.Len(t, path.Path, 3, "path %d: expected length 3", i)
		assert.Equal(t, "A", path.Path[0], "path %d: expected to start with A", i)
	}

	// Check leaf functions
	leaves := tracker.GetLeafFunctions()
	expectedLeaves := []string{"D", "E", "F", "G"}
	assert.Len(t, leaves, len(expectedLeaves))
}

// TestRecordScaleUp tests cold start tracking
func TestRecordScaleUp(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()
	now := time.Now()

	// Create function stats first
	simulateFuncExec(tracker, "funcA", 100*time.Millisecond)
	// Record a cold start
	tracker.RecordScaleUp("funcA", now, 500*time.Millisecond, true)

	// Check cold start stats
	coldStarts, lastColdStartAt, lastColdStartDuration, ok := tracker.GetColdStartStats("funcA")
	require.True(t, ok, "function stats not found")
	assert.Equal(t, 1, coldStarts)
	assert.Equal(t, now, lastColdStartAt)
	assert.Equal(t, 500*time.Millisecond, lastColdStartDuration)

	// Record another cold start
	now2 := now.Add(1 * time.Minute)
	tracker.RecordScaleUp("funcA", now2, 600*time.Millisecond, true)

	coldStarts, lastColdStartAt, lastColdStartDuration, ok = tracker.GetColdStartStats("funcA")
	require.True(t, ok)
	assert.Equal(t, 2, coldStarts)
	assert.Equal(t, now2, lastColdStartAt)
	assert.Equal(t, 600*time.Millisecond, lastColdStartDuration)

	// Record a proactive prewarm
	now3 := now2.Add(1 * time.Minute)
	tracker.RecordScaleUp("funcA", now3, 450*time.Millisecond, false)

	stats, ok := tracker.GetFunctionStats("funcA")
	require.True(t, ok)
	assert.Equal(t, 3, stats.TotalScaleUps)
	assert.Equal(t, 2, stats.TotalColdStarts)
	assert.Equal(t, 1, stats.TotalPrewarms)
	assert.Equal(t, now3, stats.LastPrewarmAt)
	assert.Equal(t, 450*time.Millisecond, stats.LastPrewarmDuration)
}

// TestGetColdStartAverage tests cold start average calculation
func TestGetColdStartAverage(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()
	now := time.Now()

	// Create function stats first
	simulateFuncExec(tracker, "funcA", 100*time.Millisecond)
	// No cold starts yet
	avg := tracker.GetColdStartAverage("funcA")
	assert.Equal(t, time.Duration(0), avg)

	// Record cold starts with different durations
	tracker.RecordScaleUp("funcA", now, 500*time.Millisecond, true)
	tracker.RecordScaleUp("funcA", now, 600*time.Millisecond, true)
	tracker.RecordScaleUp("funcA", now, 700*time.Millisecond, true)

	// Average should be (500+600+700)/3 = 600ms
	avg = tracker.GetColdStartAverage("funcA")
	assert.Equal(t, 600*time.Millisecond, avg)

	// Function stats should also reflect the average
	stats, ok := tracker.GetFunctionStats("funcA")
	require.True(t, ok)
	assert.Equal(t, 600*time.Millisecond, stats.AvgColdStartDuration)
}

// TestColdStartEmptyFunctionName tests that empty function names are rejected
func TestColdStartEmptyFunctionName(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	// Try to record cold start with empty function name
	tracker.RecordScaleUp("", time.Now(), 500*time.Millisecond, true)

	// Should not create any stats
	assert.Equal(t, 0, tracker.FunctionCount())
}

// TestColdStartInFunctionStats tests that cold start info appears in function stats
func TestColdStartInFunctionStats(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()
	now := time.Now()

	// Record function executions and cold starts
	simulateFuncExec(tracker, "funcA", 100*time.Millisecond)
	tracker.RecordScaleUp("funcA", now, 500*time.Millisecond, true)
	simulateFuncExec(tracker, "funcA", 150*time.Millisecond)

	stats, ok := tracker.GetFunctionStats("funcA")
	require.True(t, ok)

	assert.Equal(t, 2, int(stats.TotalCalls))
	assert.Equal(t, 1, stats.TotalColdStarts)
	assert.Equal(t, now, stats.LastColdStartAt)
	assert.Equal(t, 500*time.Millisecond, stats.LastColdStartDuration)
}

// TestColdStartAutoCreatesFunctionStats tests that RecordScaleUp creates function stats
// if they don't exist (e.g., for newly deployed functions)
func TestColdStartAutoCreatesFunctionStats(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()
	now := time.Now()

	// Record cold start for a function that doesn't exist yet
	// This simulates the case when a function is just deployed
	tracker.RecordScaleUp("newFunc", now, 500*time.Millisecond, true)

	// Function stats should be created
	assert.Equal(t, 1, tracker.FunctionCount())

	stats, ok := tracker.GetFunctionStats("newFunc")
	require.True(t, ok, "function stats should be auto-created")

	assert.Equal(t, 0, int(stats.TotalCalls))
	assert.Equal(t, 1, stats.TotalColdStarts)
	assert.Equal(t, now, stats.LastColdStartAt)
	assert.Equal(t, 500*time.Millisecond, stats.LastColdStartDuration)
	assert.Equal(t, 500*time.Millisecond, stats.AvgColdStartDuration)
}

// TestResetFunctionStats tests resetting function stats on redeployment
func TestResetFunctionStats(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	// Setup: create a workflow A -> B -> C with execution data
	simulateEdge(tracker, "", "funcA", 0, 100*time.Millisecond)
	simulateEdge(tracker, "funcA", "funcB", 50*time.Millisecond, 200*time.Millisecond)
	simulateEdge(tracker, "funcB", "funcC", 100*time.Millisecond, 150*time.Millisecond)

	// Add more executions to build up stats
	simulateFuncExec(tracker, "funcB", 180*time.Millisecond)
	simulateFuncExec(tracker, "funcB", 220*time.Millisecond)

	// Record cold starts for funcB
	now := time.Now()
	tracker.RecordScaleUp("funcB", now, 500*time.Millisecond, true)
	tracker.RecordScaleUp("funcB", now, 600*time.Millisecond, true)

	// Verify funcB has accumulated stats
	statsBefore, ok := tracker.GetFunctionStats("funcB")
	require.True(t, ok)
	assert.Equal(t, 3, statsBefore.TotalCalls)
	assert.Equal(t, 2, statsBefore.TotalColdStarts)
	assert.Equal(t, 0, statsBefore.TotalResets)

	// Verify edges exist
	assert.Equal(t, 3, tracker.EdgeCount())
	calleesOfA := tracker.GetCallees("funcA")
	assert.Contains(t, calleesOfA, "funcB")
	calleesOfB := tracker.GetCallees("funcB")
	assert.Contains(t, calleesOfB, "funcC")

	// Reset funcB stats (simulating redeployment)
	tracker.ResetFunctionStats("funcB")

	// Verify stats are cleared
	statsAfter, ok := tracker.GetFunctionStats("funcB")
	require.True(t, ok, "function stats should still exist after reset")

	assert.Equal(t, 0, statsAfter.TotalCalls)
	assert.Equal(t, 0, statsAfter.TotalColdStarts)
	assert.Equal(t, 0, statsAfter.TotalScaleUps)
	assert.Equal(t, 0, statsAfter.TotalPrewarms)
	assert.Equal(t, 0, statsAfter.TotalScaleDowns)
	assert.Equal(t, time.Duration(0), statsAfter.TotalExecutionTime)
	assert.Equal(t, time.Duration(0), statsAfter.MinExecutionTime)
	assert.Equal(t, time.Duration(0), statsAfter.MaxExecutionTime)
	assert.Equal(t, time.Duration(0), statsAfter.AvgExecutionTime)
	assert.Equal(t, time.Duration(0), statsAfter.AvgColdStartDuration)
	assert.True(t, statsAfter.LastPrewarmAt.IsZero())
	assert.Equal(t, time.Duration(0), statsAfter.LastPrewarmDuration)

	// Verify reset tracking
	assert.Equal(t, 1, statsAfter.TotalResets)
	assert.False(t, statsAfter.LastResetAt.IsZero())

	// Verify edges are preserved
	assert.Equal(t, 3, tracker.EdgeCount(), "edges should be preserved after reset")
	calleesOfAAfter := tracker.GetCallees("funcA")
	assert.Contains(t, calleesOfAAfter, "funcB", "edge A->B should be preserved")
	calleesOfBAfter := tracker.GetCallees("funcB")
	assert.Contains(t, calleesOfBAfter, "funcC", "edge B->C should be preserved")

	// Verify other functions are not affected
	statsA, ok := tracker.GetFunctionStats("funcA")
	require.True(t, ok)
	assert.Equal(t, 1, statsA.TotalCalls, "funcA stats should not be affected")

	statsC, ok := tracker.GetFunctionStats("funcC")
	require.True(t, ok)
	assert.Equal(t, 1, statsC.TotalCalls, "funcC stats should not be affected")
}

// TestResetFunctionStatsAndRecordNewColdStart tests that after reset,
// new cold start data is properly recorded (simulating redeployment flow)
func TestResetFunctionStatsAndRecordNewColdStart(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	now := time.Now()

	// Initial deployment: record cold start
	tracker.RecordScaleUp("funcA", now, 500*time.Millisecond, true)

	// Build up some execution history
	simulateFuncExec(tracker, "funcA", 100*time.Millisecond)
	simulateFuncExec(tracker, "funcA", 150*time.Millisecond)

	// Verify initial stats
	statsBefore, ok := tracker.GetFunctionStats("funcA")
	require.True(t, ok)
	assert.Equal(t, 2, statsBefore.TotalCalls)
	assert.Equal(t, 1, statsBefore.TotalColdStarts)
	assert.Equal(t, 500*time.Millisecond, statsBefore.AvgColdStartDuration)

	// Redeployment: reset stats first
	tracker.ResetFunctionStats("funcA")

	// Verify stats are cleared but reset tracking is updated
	statsAfterReset, ok := tracker.GetFunctionStats("funcA")
	require.True(t, ok)
	assert.Equal(t, 0, statsAfterReset.TotalCalls)
	assert.Equal(t, 0, statsAfterReset.TotalColdStarts)
	assert.Equal(t, 1, statsAfterReset.TotalResets)

	// Redeployment: record new cold start (as manager would do)
	newColdStartTime := now.Add(1 * time.Minute)
	tracker.RecordScaleUp("funcA", newColdStartTime, 400*time.Millisecond, true)

	// Verify new cold start is recorded on clean slate
	statsAfterNewColdStart, ok := tracker.GetFunctionStats("funcA")
	require.True(t, ok)
	assert.Equal(t, 0, statsAfterNewColdStart.TotalCalls) // No new executions yet
	assert.Equal(t, 1, statsAfterNewColdStart.TotalColdStarts)
	assert.Equal(t, 1, statsAfterNewColdStart.TotalResets)
	assert.Equal(t, 400*time.Millisecond, statsAfterNewColdStart.AvgColdStartDuration)
	assert.Equal(t, newColdStartTime, statsAfterNewColdStart.LastColdStartAt)
}

// TestResetFunctionStatsNonExistent tests that resetting non-existent function is safe
func TestResetFunctionStatsNonExistent(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	// Reset non-existent function should not panic or create stats
	tracker.ResetFunctionStats("nonexistent")

	assert.Equal(t, 0, tracker.FunctionCount())
}

// TestResetFunctionStatsEmptyName tests that resetting with empty name is handled
func TestResetFunctionStatsEmptyName(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	// Create some data
	simulateFuncExec(tracker, "funcA", 100*time.Millisecond)

	// Reset with empty name should not affect anything
	tracker.ResetFunctionStats("")

	stats, ok := tracker.GetFunctionStats("funcA")
	require.True(t, ok)
	assert.Equal(t, 1, stats.TotalCalls)
}

// TestResetFunctionStatsMultipleTimes tests multiple resets
func TestResetFunctionStatsMultipleTimes(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	now := time.Now()

	// Initial deployment
	tracker.RecordScaleUp("funcA", now, 500*time.Millisecond, true)
	simulateFuncExec(tracker, "funcA", 100*time.Millisecond)

	// First redeployment
	tracker.ResetFunctionStats("funcA")
	tracker.RecordScaleUp("funcA", now.Add(1*time.Minute), 400*time.Millisecond, true)

	// Second redeployment
	tracker.ResetFunctionStats("funcA")
	tracker.RecordScaleUp("funcA", now.Add(2*time.Minute), 300*time.Millisecond, true)

	// Verify reset count
	stats, ok := tracker.GetFunctionStats("funcA")
	require.True(t, ok)
	assert.Equal(t, 2, stats.TotalResets)
	assert.Equal(t, 1, stats.TotalColdStarts) // Only the latest cold start
	assert.Equal(t, 300*time.Millisecond, stats.AvgColdStartDuration)
}

// TestClearFunctionData tests full removal of function data on deletion
func TestClearFunctionData(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	// Setup: create a workflow A -> B -> C
	simulateEdge(tracker, "", "funcA", 0, 100*time.Millisecond)
	simulateEdge(tracker, "funcA", "funcB", 50*time.Millisecond, 200*time.Millisecond)
	simulateEdge(tracker, "funcB", "funcC", 100*time.Millisecond, 150*time.Millisecond)

	// Record cold starts
	now := time.Now()
	tracker.RecordScaleUp("funcB", now, 500*time.Millisecond, true)

	// Verify initial state
	assert.Equal(t, 3, tracker.FunctionCount())
	assert.Equal(t, 3, tracker.EdgeCount())
	_, ok := tracker.GetFunctionStats("funcB")
	require.True(t, ok)

	// Delete funcB
	tracker.ClearFunctionData("funcB")

	// Verify funcB is completely removed
	_, ok = tracker.GetFunctionStats("funcB")
	assert.False(t, ok, "funcB stats should be removed")

	// Verify edges involving funcB are removed
	_, ok = tracker.GetEdgeStats("funcA", "funcB")
	assert.False(t, ok, "edge A->B should be removed")
	_, ok = tracker.GetEdgeStats("funcB", "funcC")
	assert.False(t, ok, "edge B->C should be removed")

	// Verify funcB is removed from caller/callee mappings
	calleesOfA := tracker.GetCallees("funcA")
	assert.NotContains(t, calleesOfA, "funcB", "funcB should be removed from A's callees")
	callersOfC := tracker.GetCallers("funcC")
	assert.NotContains(t, callersOfC, "funcB", "funcB should be removed from C's callers")

	// Verify other functions are preserved
	assert.Equal(t, 2, tracker.FunctionCount(), "funcA and funcC should remain")
	_, ok = tracker.GetFunctionStats("funcA")
	assert.True(t, ok, "funcA should still exist")
	_, ok = tracker.GetFunctionStats("funcC")
	assert.True(t, ok, "funcC should still exist")

	// Verify external->A edge is preserved
	_, ok = tracker.GetEdgeStats("", "funcA")
	assert.True(t, ok, "edge external->A should be preserved")
}

// TestClearFunctionDataPreservesOthers tests that clearing one function doesn't affect others
func TestClearFunctionDataPreservesOthers(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	// Setup: create independent workflows
	// Workflow 1: A -> B
	// Workflow 2: X -> Y
	simulateEdge(tracker, "", "funcA", 0, 100*time.Millisecond)
	simulateEdge(tracker, "funcA", "funcB", 50*time.Millisecond, 100*time.Millisecond)
	simulateEdge(tracker, "", "funcX", 0, 100*time.Millisecond)
	simulateEdge(tracker, "funcX", "funcY", 50*time.Millisecond, 100*time.Millisecond)

	// Verify initial state
	assert.Equal(t, 4, tracker.FunctionCount())
	assert.Equal(t, 4, tracker.EdgeCount())

	// Delete funcA (removes A and edge A->B)
	tracker.ClearFunctionData("funcA")

	// Verify workflow 2 is completely unaffected
	assert.Equal(t, 3, tracker.FunctionCount()) // B, X, Y remain
	statsX, ok := tracker.GetFunctionStats("funcX")
	require.True(t, ok)
	assert.Equal(t, 1, statsX.TotalCalls)

	statsY, ok := tracker.GetFunctionStats("funcY")
	require.True(t, ok)
	assert.Equal(t, 1, statsY.TotalCalls)

	edgeXY, ok := tracker.GetEdgeStats("funcX", "funcY")
	require.True(t, ok)
	assert.Equal(t, 1, edgeXY.Count)
}

// TestClearFunctionDataEdgeCleanup tests bidirectional edge cleanup
func TestClearFunctionDataEdgeCleanup(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	// Setup: funcB is called by multiple callers and calls multiple callees
	// A -> B, X -> B, B -> C, B -> Y
	simulateEdge(tracker, "", "funcA", 0, 100*time.Millisecond)
	simulateEdge(tracker, "", "funcX", 0, 100*time.Millisecond)
	simulateEdge(tracker, "funcA", "funcB", 50*time.Millisecond, 100*time.Millisecond)
	simulateEdge(tracker, "funcX", "funcB", 50*time.Millisecond, 100*time.Millisecond)
	simulateEdge(tracker, "funcB", "funcC", 50*time.Millisecond, 100*time.Millisecond)
	simulateEdge(tracker, "funcB", "funcY", 50*time.Millisecond, 100*time.Millisecond)

	// Verify B has multiple callers and callees
	callersOfB := tracker.GetCallers("funcB")
	assert.Len(t, callersOfB, 2) // A, X
	calleesOfB := tracker.GetCallees("funcB")
	assert.Len(t, calleesOfB, 2) // C, Y

	// Delete funcB
	tracker.ClearFunctionData("funcB")

	// All edges involving B should be removed
	_, ok := tracker.GetEdgeStats("funcA", "funcB")
	assert.False(t, ok)
	_, ok = tracker.GetEdgeStats("funcX", "funcB")
	assert.False(t, ok)
	_, ok = tracker.GetEdgeStats("funcB", "funcC")
	assert.False(t, ok)
	_, ok = tracker.GetEdgeStats("funcB", "funcY")
	assert.False(t, ok)

	// B should be removed from all caller/callee lists
	calleesOfA := tracker.GetCallees("funcA")
	assert.NotContains(t, calleesOfA, "funcB")
	calleesOfX := tracker.GetCallees("funcX")
	assert.NotContains(t, calleesOfX, "funcB")
	callersOfC := tracker.GetCallers("funcC")
	assert.NotContains(t, callersOfC, "funcB")
	callersOfY := tracker.GetCallers("funcY")
	assert.NotContains(t, callersOfY, "funcB")

	// Other functions should still exist
	assert.Equal(t, 4, tracker.FunctionCount()) // A, X, C, Y
}

// TestClearFunctionDataNonExistent tests that clearing non-existent function is safe
func TestClearFunctionDataNonExistent(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	// Create some data
	simulateFuncExec(tracker, "funcA", 100*time.Millisecond)

	// Clear non-existent function should not panic or affect existing data
	tracker.ClearFunctionData("nonexistent")

	assert.Equal(t, 1, tracker.FunctionCount())
	stats, ok := tracker.GetFunctionStats("funcA")
	require.True(t, ok)
	assert.Equal(t, 1, stats.TotalCalls)
}

// TestClearFunctionDataEmptyName tests that empty name is handled
func TestClearFunctionDataEmptyName(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	// Create some data
	simulateFuncExec(tracker, "funcA", 100*time.Millisecond)

	// Clear with empty name should not affect anything
	tracker.ClearFunctionData("")

	assert.Equal(t, 1, tracker.FunctionCount())
}

// TestClearFunctionDataWithActiveContexts tests cleanup of in-flight execution contexts
func TestClearFunctionDataWithActiveContexts(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	requestID := "req-active"
	now := time.Now()

	// Start execution but don't end it (simulating in-flight request)
	tracker.StartExecution("funcA", requestID, "exec-active", now)

	// Verify context exists
	tracker.mutex.RLock()
	assert.NotNil(t, tracker.executionContexts[requestID])
	assert.NotNil(t, tracker.executionContexts[requestID]["exec-active"])
	tracker.mutex.RUnlock()

	// Delete the function while request is in-flight
	tracker.ClearFunctionData("funcA")

	// Context for funcA should be cleaned up
	tracker.mutex.RLock()
	if ctx, ok := tracker.executionContexts[requestID]; ok {
		_, funcExists := ctx["exec-active"]
		assert.False(t, funcExists, "funcA should be removed from execution context")
	}
	tracker.mutex.RUnlock()
}

// TestClearFunctionDataPrewarmNoLongerTargets tests that deleted function is not a prewarm target
func TestClearFunctionDataPrewarmNoLongerTargets(t *testing.T) {
	config := DefaultConfig()
	config.Prewarm.Enabled = true

	tracker := New(WithConfig(config), WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	now := time.Now()

	// Setup: A -> B with prewarm data
	for i := 0; i < 3; i++ {
		simulateEdge(tracker, "funcA", "funcB", 500*time.Millisecond, 50*time.Millisecond)
	}
	for i := 0; i < 3; i++ {
		tracker.RecordScaleUp("funcB", now, 300*time.Millisecond, true)
	}

	// Verify B is a prewarm target before deletion
	targets := tracker.GetPrewarmTargets("funcA")
	require.Len(t, targets, 1)
	assert.Equal(t, "funcB", targets[0].FunctionName)

	// Delete funcB
	tracker.ClearFunctionData("funcB")

	// B should no longer be a prewarm target
	targets = tracker.GetPrewarmTargets("funcA")
	assert.Empty(t, targets, "deleted function should not be a prewarm target")
}

// TestStartExecutionAndRecordEdge tests the requestID-based API
func TestStartExecutionAndRecordEdge(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	requestID := "req-123"
	now := time.Now()

	// Start execution of funcA
	tracker.StartExecution("funcA", requestID, "exec-a", now)

	// 100ms later, funcA calls funcB
	callTime := now.Add(100 * time.Millisecond)
	tracker.RecordEdge("funcA", "funcB", requestID, "exec-a", callTime)

	// Verify edge was recorded with correct execution time
	assert.Equal(t, 1, tracker.EdgeCount())

	stats, ok := tracker.GetEdgeStats("funcA", "funcB")
	require.True(t, ok, "edge stats not found")

	assert.Equal(t, 1, stats.Count)
	assert.Equal(t, 100*time.Millisecond, stats.TotalExecutionTime)
	assert.Equal(t, 100*time.Millisecond, stats.AvgExecutionTime)
}

// TestRecordEdgeExternalEntry tests external calls (no caller)
func TestRecordEdgeExternalEntry(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	requestID := "req-456"
	now := time.Now()

	// External call to funcA (no StartExecution needed for caller)
	tracker.RecordEdge("", "funcA", requestID, "", now)

	// Verify edge was recorded
	assert.Equal(t, 1, tracker.EdgeCount())

	stats, ok := tracker.GetEdgeStats("", "funcA")
	require.True(t, ok, "edge stats not found")

	assert.Equal(t, 1, stats.Count)
	assert.Equal(t, time.Duration(0), stats.TotalExecutionTime)
}

// TestRecordEdgeChain tests multiple calls in a chain
func TestRecordEdgeChain(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	requestID := "req-789"
	t0 := time.Now()

	// External → funcA
	tracker.RecordEdge("", "funcA", requestID, "", t0)
	tracker.StartExecution("funcA", requestID, "exec-a", t0)

	// 50ms later: funcA → funcB
	t1 := t0.Add(50 * time.Millisecond)
	tracker.RecordEdge("funcA", "funcB", requestID, "exec-a", t1)
	tracker.StartExecution("funcB", requestID, "exec-b", t1)

	// 30ms later: funcB → funcC
	t2 := t1.Add(30 * time.Millisecond)
	tracker.RecordEdge("funcB", "funcC", requestID, "exec-b", t2)
	tracker.StartExecution("funcC", requestID, "exec-c", t2)

	// Verify all edges
	assert.Equal(t, 3, tracker.EdgeCount())

	// Check edge A→B (should be 50ms from A start to A calling B)
	statsAB, ok := tracker.GetEdgeStats("funcA", "funcB")
	require.True(t, ok)
	assert.Equal(t, 50*time.Millisecond, statsAB.AvgExecutionTime)

	// Check edge B→C (should be 30ms from B start to B calling C)
	statsBC, ok := tracker.GetEdgeStats("funcB", "funcC")
	require.True(t, ok)
	assert.Equal(t, 30*time.Millisecond, statsBC.AvgExecutionTime)
}

// TestConcurrentRequestsWithRequestID tests multiple concurrent workflows
func TestConcurrentRequestsWithRequestID(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	now := time.Now()

	// Request 1: A → B (100ms edge time)
	req1 := "req-001"
	tracker.StartExecution("funcA", req1, "exec-1", now)
	tracker.RecordEdge("funcA", "funcB", req1, "exec-1", now.Add(100*time.Millisecond))

	// Request 2: A → B (200ms edge time)
	req2 := "req-002"
	tracker.StartExecution("funcA", req2, "exec-2", now)
	tracker.RecordEdge("funcA", "funcB", req2, "exec-2", now.Add(200*time.Millisecond))

	// Request 3: A → C (150ms edge time)
	req3 := "req-003"
	tracker.StartExecution("funcA", req3, "exec-3", now)
	tracker.RecordEdge("funcA", "funcC", req3, "exec-3", now.Add(150*time.Millisecond))

	// Verify edges (2 unique edge relationships: A→B and A→C)
	assert.Equal(t, 2, tracker.EdgeCount())

	// Edge A→B should have average of 150ms (100+200)/2
	statsAB, ok := tracker.GetEdgeStats("funcA", "funcB")
	require.True(t, ok)
	assert.Equal(t, 2, statsAB.Count)
	assert.Equal(t, 300*time.Millisecond, statsAB.TotalExecutionTime)
	assert.Equal(t, 150*time.Millisecond, statsAB.AvgExecutionTime)

	// Edge A→C should have 150ms
	statsAC, ok := tracker.GetEdgeStats("funcA", "funcC")
	require.True(t, ok)
	assert.Equal(t, 1, statsAC.Count)
	assert.Equal(t, 150*time.Millisecond, statsAC.AvgExecutionTime)
}

// TestEndExecution tests cleanup of execution contexts and function stats recording
func TestEndExecution(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	requestID := "req-cleanup"
	now := time.Now()

	// Start execution
	tracker.StartExecution("funcA", requestID, "exec-1", now)

	// Verify context exists
	tracker.mutex.RLock()
	assert.NotNil(t, tracker.executionContexts[requestID])
	tracker.mutex.RUnlock()

	// End execution
	endTime := now.Add(100 * time.Millisecond)
	tracker.EndExecution("funcA", requestID, "exec-1", endTime)

	// Verify context is cleaned up
	tracker.mutex.RLock()
	assert.Nil(t, tracker.executionContexts[requestID])
	tracker.mutex.RUnlock()

	// Verify function stats were recorded
	stats, ok := tracker.GetFunctionStats("funcA")
	require.True(t, ok, "function stats should be recorded by EndExecution")
	assert.Equal(t, 1, int(stats.TotalCalls))
	assert.Equal(t, 100*time.Millisecond, stats.TotalExecutionTime)
}

// TestRecordEdgeWithoutStartExecution tests error handling
func TestRecordEdgeWithoutStartExecution(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	requestID := "req-missing"
	now := time.Now()

	// Try to record edge without StartExecution
	tracker.RecordEdge("funcA", "funcB", requestID, "exec-missing", now)

	// Should not record the edge since caller start time is unknown
	assert.Equal(t, 0, tracker.EdgeCount())
}

// TestConcurrentSameFunctionExecutionIDIsolation ensures concurrent invocations under same requestID do not collide
func TestConcurrentSameFunctionExecutionIDIsolation(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	requestID := "req-concurrent"
	now := time.Now()

	tracker.StartExecution("funcA", requestID, "exec-1", now)
	tracker.StartExecution("funcA", requestID, "exec-2", now.Add(10*time.Millisecond))

	tracker.EndExecution("funcA", requestID, "exec-1", now.Add(100*time.Millisecond))
	tracker.EndExecution("funcA", requestID, "exec-2", now.Add(120*time.Millisecond))

	stats, ok := tracker.GetFunctionStats("funcA")
	require.True(t, ok)
	assert.Equal(t, 2, stats.TotalCalls)

	tracker.mutex.RLock()
	_, exists := tracker.executionContexts[requestID]
	tracker.mutex.RUnlock()
	assert.False(t, exists, "execution context should be cleaned up after both invocations")
}

// TestMultipleFunctionsInSameRequest tests complex workflow
func TestMultipleFunctionsInSameRequest(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	requestID := "req-complex"
	t0 := time.Now()

	// Workflow: External → A → B
	//                       A → C (A calls both B and C)

	// External call to A
	tracker.RecordEdge("", "A", requestID, "", t0)
	tracker.StartExecution("A", requestID, "exec-a", t0)

	// A calls B at t=50ms
	t1 := t0.Add(50 * time.Millisecond)
	tracker.RecordEdge("A", "B", requestID, "exec-a", t1)
	tracker.StartExecution("B", requestID, "exec-b", t1)

	// A calls C at t=100ms (A started at t=0)
	t2 := t0.Add(100 * time.Millisecond)
	tracker.RecordEdge("A", "C", requestID, "exec-a", t2)
	tracker.StartExecution("C", requestID, "exec-c", t2)

	// Verify edges
	assert.Equal(t, 3, tracker.EdgeCount())

	// Edge A→B: 50ms (from A start to A calling B)
	statsAB, ok := tracker.GetEdgeStats("A", "B")
	require.True(t, ok)
	assert.Equal(t, 50*time.Millisecond, statsAB.AvgExecutionTime)

	// Edge A→C: 100ms (from A start to A calling C)
	statsAC, ok := tracker.GetEdgeStats("A", "C")
	require.True(t, ok)
	assert.Equal(t, 100*time.Millisecond, statsAC.AvgExecutionTime)

	// Verify caller/callee relationships
	callees := tracker.GetCallees("A")
	assert.ElementsMatch(t, []string{"B", "C"}, callees)
}

// TestContextCleanup tests that stale execution contexts are cleaned up
func TestContextCleanup(t *testing.T) {
	// Create tracker with short TTL for testing
	config := DefaultConfig()
	config.ContextTTL = 100 * time.Millisecond
	config.ContextCleanupInterval = 50 * time.Millisecond

	tracker := New(WithConfig(config), WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	requestID := "req-cleanup-test"
	now := time.Now()

	// Start execution but don't end it
	tracker.StartExecution("funcA", requestID, "exec-cleanup", now)

	// Verify context exists
	tracker.mutex.RLock()
	assert.NotNil(t, tracker.executionContexts[requestID])
	assert.NotNil(t, tracker.contextLastAccess[requestID])
	tracker.mutex.RUnlock()

	// Wait for cleanup to run (TTL + cleanup interval + buffer)
	time.Sleep(200 * time.Millisecond)

	// Verify context is cleaned up due to TTL expiry
	tracker.mutex.RLock()
	_, contextExists := tracker.executionContexts[requestID]
	_, accessExists := tracker.contextLastAccess[requestID]
	tracker.mutex.RUnlock()

	assert.False(t, contextExists, "execution context should be cleaned up after TTL")
	assert.False(t, accessExists, "context last access should be cleaned up after TTL")
}

// TestTrackerStop tests graceful shutdown of the tracker
func TestTrackerStop(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()

	// Stop should complete without hanging
	done := make(chan struct{})
	go func() {
		tracker.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Success - Stop completed
	case <-time.After(2 * time.Second):
		t.Fatal("tracker.Stop() did not complete in time")
	}
}

// TestGetPrewarmTargets tests getting prewarm targets for a function
func TestGetPrewarmTargets(t *testing.T) {
	config := DefaultConfig()
	config.Prewarm.Enabled = true

	tracker := New(WithConfig(config), WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	now := time.Now()

	// No data - no targets
	targets := tracker.GetPrewarmTargets("funcA")
	assert.Empty(t, targets)

	// Setup workflow: A -> B, A -> C
	// B should be prewarmed (long edge time, short cold start)
	// C should NOT be prewarmed (short edge time, long cold start)

	// A -> B: 500ms edge time
	for i := 0; i < 3; i++ {
		simulateEdge(tracker, "funcA", "funcB", 500*time.Millisecond, 50*time.Millisecond)
	}

	// A -> C: 100ms edge time
	for i := 0; i < 3; i++ {
		simulateEdge(tracker, "funcA", "funcC", 100*time.Millisecond, 50*time.Millisecond)
	}

	// B: 300ms cold start
	for i := 0; i < 3; i++ {
		tracker.RecordScaleUp("funcB", now, 300*time.Millisecond, true)
	}

	// C: 500ms cold start
	for i := 0; i < 3; i++ {
		tracker.RecordScaleUp("funcC", now, 500*time.Millisecond, true)
	}

	targets = tracker.GetPrewarmTargets("funcA")

	// Both should be in targets with LeadTime = avgEdgeTime (time until callee invocation)
	// The prewarm scheduler will compute: delay = LeadTime - coldStartTime - margin
	require.Len(t, targets, 2)

	// Build a map for order-independent assertions
	targetMap := make(map[string]time.Duration)
	for _, t := range targets {
		targetMap[t.FunctionName] = t.LeadTime
	}

	assert.Contains(t, targetMap, "funcB")
	assert.Contains(t, targetMap, "funcC")
	assert.Equal(t, 500*time.Millisecond, targetMap["funcB"]) // avgEdgeTime for A->B
	assert.Equal(t, 100*time.Millisecond, targetMap["funcC"]) // avgEdgeTime for A->C
}

// TestPrewarmDisabled tests that prewarming returns empty when disabled
func TestPrewarmDisabled(t *testing.T) {
	config := DefaultConfig()
	config.Prewarm.Enabled = false

	tracker := New(WithConfig(config), WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	now := time.Now()

	// Add data that would normally trigger prewarming
	for i := 0; i < 3; i++ {
		simulateEdge(tracker, "funcA", "funcB", 500*time.Millisecond, 50*time.Millisecond)
	}
	for i := 0; i < 3; i++ {
		tracker.RecordScaleUp("funcB", now, 300*time.Millisecond, true)
	}

	targets := tracker.GetPrewarmTargets("funcA")
	assert.Empty(t, targets)
}

func TestRecordPrewarmInFunctionStats(t *testing.T) {
	tracker := New(WithLogger(zap.NewNop()))
	tracker.Start()
	defer tracker.Stop()

	now := time.Now()

	tracker.RecordScaleUp("funcA", now, 300*time.Millisecond, false)

	stats, ok := tracker.GetFunctionStats("funcA")
	require.True(t, ok)
	assert.Equal(t, 1, stats.TotalScaleUps)
	assert.Equal(t, 0, stats.TotalColdStarts)
	assert.Equal(t, 1, stats.TotalPrewarms)
	assert.Equal(t, now, stats.LastPrewarmAt)
	assert.Equal(t, 300*time.Millisecond, stats.LastPrewarmDuration)
}
