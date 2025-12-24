package callgraph

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewTracker(t *testing.T) {
	tracker := New()
	assert.Equal(t, 0, tracker.EdgeCount())
	assert.Equal(t, 0, tracker.FunctionCount())
	assert.Equal(t, SimpleMovingAverage, tracker.averagingMethod)
}

func TestNewTrackerWithConfig(t *testing.T) {
	config := &Config{
		MaxEdges: 100,
	}
	tracker := New(
		WithConfig(config),
		WithLogger(zap.NewNop()),
	)
	assert.Equal(t, 100, tracker.config.MaxEdges)
	assert.Equal(t, SimpleMovingAverage, tracker.averagingMethod)
}

func TestRecord(t *testing.T) {
	tracker := New()

	// Record a call edge and function execution
	tracker.RecordEdge("funcA", "funcB", 100*time.Millisecond)
	tracker.RecordFuncExec("funcB", time.Now(), 100*time.Millisecond)

	// Check edge count
	assert.Equal(t, 1, tracker.EdgeCount())

	// Check function count
	assert.Equal(t, 1, tracker.FunctionCount())

	// Check callees
	callees := tracker.GetCallees("funcA")
	assert.Equal(t, []string{"funcB"}, callees)

	// Check callers
	callers := tracker.GetCallers("funcB")
	assert.Equal(t, []string{"funcA"}, callers)
}

func TestRecordExternalCall(t *testing.T) {
	tracker := New()

	// Record an external call (empty caller)
	tracker.RecordEdge("", "funcA", 50*time.Millisecond)

	// Check that external calls are tracked
	callees := tracker.GetCallees("")
	assert.Equal(t, []string{"funcA"}, callees)
}

func TestRecordCall(t *testing.T) {
	tracker := New()

	// Record a call without execution time
	tracker.RecordEdgeCall("funcA", "funcB")
	tracker.RecordFuncExec("funcB", time.Now(), 0)

	assert.Equal(t, 1, tracker.EdgeCount())

	// Verify execution time is 0
	stats, ok := tracker.GetFunctionStats("funcB")
	require.True(t, ok, "function stats not found")
	assert.Equal(t, time.Duration(0), stats.AvgExecutionTime)
}

func TestGetFunctionStats(t *testing.T) {
	tracker := New()

	// Record multiple function executions
	now := time.Now()
	tracker.RecordFuncExec("funcA", now, 100*time.Millisecond)
	tracker.RecordFuncExec("funcA", now, 200*time.Millisecond)
	tracker.RecordFuncExec("funcA", now, 150*time.Millisecond)

	stats, ok := tracker.GetFunctionStats("funcA")
	require.True(t, ok, "function stats not found")

	assert.Equal(t, 3, int(stats.TotalCalls))
	assert.Equal(t, 450*time.Millisecond, stats.TotalExecutionTime)
	assert.Equal(t, 100*time.Millisecond, stats.MinExecutionTime)
	assert.Equal(t, 200*time.Millisecond, stats.MaxExecutionTime)
}

func TestGetEdgeStats(t *testing.T) {
	tracker := New()

	tracker.RecordEdge("funcA", "funcB", 100*time.Millisecond)
	tracker.RecordEdge("funcA", "funcB", 200*time.Millisecond)

	stats, ok := tracker.GetEdgeStats("funcA", "funcB")
	require.True(t, ok, "edge stats not found")

	assert.Equal(t, 2, int(stats.Count))
	assert.Equal(t, 300*time.Millisecond, stats.TotalExecutionTime)
}

func TestGetCallGraph(t *testing.T) {
	tracker := New()
	now := time.Now()

	tracker.RecordEdge("", "funcA", 50*time.Millisecond)
	tracker.RecordFuncExec("funcA", now, 50*time.Millisecond)
	tracker.RecordEdge("funcA", "funcB", 100*time.Millisecond)
	tracker.RecordFuncExec("funcB", now, 100*time.Millisecond)
	tracker.RecordEdge("funcA", "funcC", 150*time.Millisecond)
	tracker.RecordFuncExec("funcC", now, 150*time.Millisecond)

	graph := tracker.GetCallGraph()

	assert.Len(t, graph.Edges, 3)
	assert.Len(t, graph.Functions, 3)
	assert.Equal(t, 3, int(graph.TotalCalls))
}

func TestMaxEdgesLimit(t *testing.T) {
	config := DefaultConfig()
	config.MaxEdges = 5
	tracker := New(
		WithConfig(config),
		WithLogger(zap.NewNop()),
	)

	// Record more edges than the limit
	for i := 0; i < 10; i++ {
		tracker.RecordEdge("funcA", "funcB", time.Millisecond)
	}

	assert.Equal(t, 5, tracker.EdgeCount())
}

func TestClear(t *testing.T) {
	tracker := New()

	tracker.RecordEdge("funcA", "funcB", 100*time.Millisecond)
	tracker.RecordEdge("funcB", "funcC", 200*time.Millisecond)

	tracker.Clear()

	assert.Equal(t, 0, tracker.EdgeCount())
	assert.Equal(t, 0, tracker.FunctionCount())
}

func TestToJSON(t *testing.T) {
	tracker := New()

	tracker.RecordEdge("", "funcA", 100*time.Millisecond)
	tracker.RecordEdge("funcA", "funcB", 200*time.Millisecond)

	data, err := tracker.ToJSON()
	require.NoError(t, err)

	var graph CallGraph
	err = json.Unmarshal(data, &graph)
	require.NoError(t, err)

	assert.Len(t, graph.Edges, 2)
}

func TestFromJSON(t *testing.T) {
	tracker := New()
	now := time.Now()

	tracker.RecordEdge("", "funcA", 100*time.Millisecond)
	tracker.RecordFuncExec("funcA", now, 100*time.Millisecond)
	tracker.RecordEdge("funcA", "funcB", 200*time.Millisecond)
	tracker.RecordFuncExec("funcB", now, 200*time.Millisecond)

	data, err := tracker.ToJSON()
	require.NoError(t, err)

	// Create new tracker and load from JSON
	newTracker := New()
	err = newTracker.FromJSON(data)
	require.NoError(t, err)

	// Verify data was loaded
	assert.Equal(t, 2, newTracker.UniqueEdgeCount())
	assert.Equal(t, 2, newTracker.FunctionCount())
}

func TestGetCallPaths(t *testing.T) {
	tracker := New()

	// Create a workflow: external -> A -> B -> C
	tracker.RecordEdge("", "funcA", 100*time.Millisecond)
	tracker.RecordEdge("funcA", "funcB", 200*time.Millisecond)
	tracker.RecordEdge("funcB", "funcC", 150*time.Millisecond)

	paths := tracker.GetCallPaths()

	require.Len(t, paths, 1)
	assert.Len(t, paths[0].Path, 3)
	assert.Equal(t, []string{"funcA", "funcB", "funcC"}, paths[0].Path)
}

func TestGetPathsContaining(t *testing.T) {
	tracker := New()

	tracker.RecordEdge("", "funcA", 100*time.Millisecond)
	tracker.RecordEdge("funcA", "funcB", 200*time.Millisecond)
	tracker.RecordEdge("funcA", "funcC", 150*time.Millisecond)

	paths := tracker.GetPathsContaining("funcB")

	assert.Len(t, paths, 1)
}

func TestGetLongestPath(t *testing.T) {
	tracker := New()

	// Path 1: A -> B (length 2)
	tracker.RecordEdge("", "funcA", 100*time.Millisecond)
	tracker.RecordEdge("funcA", "funcB", 200*time.Millisecond)

	// Path 2: X -> Y -> Z (length 3) - separate entry point
	tracker.RecordEdge("", "funcX", 100*time.Millisecond)
	tracker.RecordEdge("funcX", "funcY", 200*time.Millisecond)
	tracker.RecordEdge("funcY", "funcZ", 150*time.Millisecond)

	longest := tracker.GetLongestPath()

	assert.Len(t, longest.Path, 3)
}

func TestGetSlowestPath(t *testing.T) {
	tracker := New()

	// Fast path
	tracker.RecordEdge("", "fastA", 10*time.Millisecond)
	tracker.RecordFuncExec("fastA", time.Now(), 10*time.Millisecond)
	tracker.RecordEdge("fastA", "fastB", 10*time.Millisecond)
	tracker.RecordFuncExec("fastB", time.Now(), 10*time.Millisecond)

	// Slow path
	tracker.RecordEdge("", "slowA", 500*time.Millisecond)
	tracker.RecordFuncExec("slowA", time.Now(), 500*time.Millisecond)
	tracker.RecordEdge("slowA", "slowB", 500*time.Millisecond)
	tracker.RecordFuncExec("slowB", time.Now(), 500*time.Millisecond)

	slowest := tracker.GetSlowestPath()

	require.NotEmpty(t, slowest.Path)
	assert.Equal(t, "slowA", slowest.Path[0])
}

func TestGetAverageExecutionTime(t *testing.T) {
	tracker := New()
	now := time.Now()

	tracker.RecordFuncExec("funcA", now, 100*time.Millisecond)
	tracker.RecordFuncExec("funcA", now, 200*time.Millisecond)
	tracker.RecordFuncExec("funcA", now, 300*time.Millisecond)

	avg := tracker.GetAverageExecutionTime("funcA")

	// SMA with 3 samples: (100+200+300)/3 = 200ms
	expected := int64(200 * time.Millisecond)
	assert.Equal(t, expected, avg)
}

func TestConcurrentAccess(t *testing.T) {
	tracker := New()
	done := make(chan bool)

	// Concurrent writers
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				tracker.RecordEdge("funcA", "funcB", time.Millisecond)
				tracker.RecordFuncExec("funcB", time.Now(), time.Millisecond)
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
	var _ Serializer = (*CallGraphTracker)(nil)
	var _ PathAnalyzer = (*CallGraphTracker)(nil)
	var _ FullTracker = (*CallGraphTracker)(nil)
}

// TestDAGWithBranches tests a DAG topology where one function calls multiple functions
// Graph: external -> A -> B
//
//	A -> C
func TestDAGWithBranches(t *testing.T) {
	tracker := New()

	// A calls both B and C (branching)
	tracker.RecordEdge("", "A", 100*time.Millisecond)
	tracker.RecordEdge("A", "B", 50*time.Millisecond)
	tracker.RecordEdge("A", "C", 75*time.Millisecond)

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
	tracker := New()

	// Create diamond: A -> B -> D and A -> C -> D
	tracker.RecordEdge("", "A", 100*time.Millisecond)
	tracker.RecordEdge("A", "B", 50*time.Millisecond)
	tracker.RecordEdge("A", "C", 75*time.Millisecond)
	tracker.RecordEdge("B", "D", 25*time.Millisecond)
	tracker.RecordEdge("C", "D", 30*time.Millisecond)

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
	tracker := New()

	// Two entry points: X and Y, both eventually reach Z
	tracker.RecordEdge("", "X", 100*time.Millisecond)
	tracker.RecordEdge("", "Y", 100*time.Millisecond)
	tracker.RecordEdge("X", "Z", 50*time.Millisecond)
	tracker.RecordEdge("Y", "Z", 50*time.Millisecond)

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
	tracker := New()

	tracker.RecordEdge("", "A", 100*time.Millisecond)
	tracker.RecordFuncExec("A", time.Now(), 100*time.Millisecond)
	tracker.RecordEdge("A", "B", 50*time.Millisecond)
	tracker.RecordFuncExec("B", time.Now(), 50*time.Millisecond)
	tracker.RecordEdge("A", "C", 60*time.Millisecond)
	tracker.RecordFuncExec("C", time.Now(), 60*time.Millisecond)
	tracker.RecordEdge("B", "D", 40*time.Millisecond)
	tracker.RecordFuncExec("D", time.Now(), 40*time.Millisecond)
	tracker.RecordEdge("C", "E", 45*time.Millisecond)
	tracker.RecordFuncExec("E", time.Now(), 45*time.Millisecond)
	tracker.RecordEdge("D", "F", 30*time.Millisecond)
	tracker.RecordFuncExec("F", time.Now(), 30*time.Millisecond)
	tracker.RecordEdge("E", "F", 35*time.Millisecond)
	tracker.RecordEdge("E", "G", 25*time.Millisecond)
	tracker.RecordFuncExec("G", time.Now(), 25*time.Millisecond)

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
	tracker := New()

	tracker.RecordEdge("", "A", 100*time.Millisecond)
	tracker.RecordEdge("", "B", 100*time.Millisecond)
	tracker.RecordEdge("A", "C", 50*time.Millisecond)
	tracker.RecordEdge("B", "C", 50*time.Millisecond)

	entryPoints := tracker.GetEntryPoints()

	assert.Len(t, entryPoints, 2)
}

func TestGetLeafFunctions(t *testing.T) {
	tracker := New()

	tracker.RecordEdge("", "A", 100*time.Millisecond)
	tracker.RecordFuncExec("A", time.Now(), 100*time.Millisecond)
	tracker.RecordEdge("A", "B", 50*time.Millisecond)
	tracker.RecordFuncExec("B", time.Now(), 50*time.Millisecond)
	tracker.RecordEdge("A", "C", 75*time.Millisecond)
	tracker.RecordFuncExec("C", time.Now(), 75*time.Millisecond)
	// B and C are leaves (they don't call anything)

	leaves := tracker.GetLeafFunctions()

	assert.Len(t, leaves, 2)
}

func TestGetDownstreamFunctions(t *testing.T) {
	tracker := New()

	// A -> B -> D
	// A -> C -> D
	tracker.RecordEdge("", "A", 100*time.Millisecond)
	tracker.RecordEdge("A", "B", 50*time.Millisecond)
	tracker.RecordEdge("A", "C", 75*time.Millisecond)
	tracker.RecordEdge("B", "D", 25*time.Millisecond)
	tracker.RecordEdge("C", "D", 30*time.Millisecond)

	downstream := tracker.GetDownstreamFunctions("A")

	// A can reach B, C, D
	assert.Len(t, downstream, 3)

	downstreamFromB := tracker.GetDownstreamFunctions("B")
	assert.Equal(t, []string{"D"}, downstreamFromB)
}

func TestGetUpstreamFunctions(t *testing.T) {
	tracker := New()

	// A -> B -> D
	// A -> C -> D
	tracker.RecordEdge("", "A", 100*time.Millisecond)
	tracker.RecordEdge("A", "B", 50*time.Millisecond)
	tracker.RecordEdge("A", "C", 75*time.Millisecond)
	tracker.RecordEdge("B", "D", 25*time.Millisecond)
	tracker.RecordEdge("C", "D", 30*time.Millisecond)

	upstream := tracker.GetUpstreamFunctions("D")

	// D can be reached from A, B, C
	assert.Len(t, upstream, 3)

	upstreamToA := tracker.GetUpstreamFunctions("A")
	assert.Empty(t, upstreamToA)
}

func TestGetSubgraph(t *testing.T) {
	tracker := New()

	tracker.RecordEdge("", "A", 100*time.Millisecond)
	tracker.RecordFuncExec("A", time.Now(), 100*time.Millisecond)
	tracker.RecordEdge("A", "B", 50*time.Millisecond)
	tracker.RecordFuncExec("B", time.Now(), 50*time.Millisecond)
	tracker.RecordEdge("A", "C", 75*time.Millisecond)
	tracker.RecordFuncExec("C", time.Now(), 75*time.Millisecond)
	tracker.RecordEdge("B", "D", 25*time.Millisecond)
	tracker.RecordFuncExec("D", time.Now(), 25*time.Millisecond)

	// Get subgraph with only A, B, D
	subgraph := tracker.GetSubgraph([]string{"A", "B", "D"})

	// Should have 3 edges: ""->A (external), A->B, and B->D (A->C excluded because C is not in the list)
	if !assert.Len(t, subgraph.Edges, 3, "expected 3 edges in subgraph (including external entry)") {
		for _, e := range subgraph.Edges {
			t.Logf("Edge: %s -> %s", e.Caller, e.Callee)
		}
	}

	assert.Len(t, subgraph.Functions, 3)
}

// TestSelfLoopPrevention tests that self-loops are prevented
func TestSelfLoopPrevention(t *testing.T) {
	tracker := New()

	// Try to record a self-loop
	tracker.RecordEdge("funcA", "funcA", 100*time.Millisecond)

	// Should have no edges recorded
	assert.Equal(t, 0, tracker.EdgeCount())

	// Should have no functions recorded
	assert.Equal(t, 0, tracker.FunctionCount())

	// External call to self should be recorded (not a self-loop since caller is empty)
	tracker.RecordEdge("", "funcA", 100*time.Millisecond)
	assert.Equal(t, 1, tracker.EdgeCount())

	// Try self-loop again after legitimate call
	tracker.RecordEdge("funcA", "funcA", 100*time.Millisecond)
	assert.Equal(t, 1, tracker.EdgeCount())
}

// TestEmptyCalleeValidation tests that empty callee is rejected
func TestEmptyCalleeValidation(t *testing.T) {
	tracker := New()

	// Try to record with empty callee
	tracker.RecordEdge("funcA", "", 100*time.Millisecond)

	// Should have no edges recorded
	assert.Equal(t, 0, tracker.EdgeCount())

	// Try external call with empty callee
	tracker.RecordEdge("", "", 100*time.Millisecond)
	assert.Equal(t, 0, tracker.EdgeCount())

	// Empty caller is valid (external call)
	tracker.RecordEdge("", "funcA", 100*time.Millisecond)
	assert.Equal(t, 1, tracker.EdgeCount())
}

// TestPathTraversalWithPotentialCycle tests the fixed path traversal
func TestPathTraversalWithPotentialCycle(t *testing.T) {
	tracker := New()

	// Create a complex branching structure
	// Entry -> A -> B -> D
	//       -> A -> C -> D
	// Both B and C call D, but D should appear in both paths
	tracker.RecordEdge("", "A", 100*time.Millisecond)
	tracker.RecordEdge("A", "B", 50*time.Millisecond)
	tracker.RecordEdge("A", "C", 50*time.Millisecond)
	tracker.RecordEdge("B", "D", 25*time.Millisecond)
	tracker.RecordEdge("C", "D", 25*time.Millisecond)

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
	tracker := New()

	// Create a tree structure:
	//       A
	//      / \
	//     B   C
	//    / \ / \
	//   D  E F  G
	tracker.RecordEdge("", "A", 100*time.Millisecond)
	tracker.RecordFuncExec("A", time.Now(), 100*time.Millisecond)
	tracker.RecordEdge("A", "B", 50*time.Millisecond)
	tracker.RecordFuncExec("B", time.Now(), 50*time.Millisecond)
	tracker.RecordEdge("A", "C", 50*time.Millisecond)
	tracker.RecordFuncExec("C", time.Now(), 50*time.Millisecond)
	tracker.RecordEdge("B", "D", 25*time.Millisecond)
	tracker.RecordFuncExec("D", time.Now(), 25*time.Millisecond)
	tracker.RecordEdge("B", "E", 25*time.Millisecond)
	tracker.RecordFuncExec("E", time.Now(), 25*time.Millisecond)
	tracker.RecordEdge("C", "F", 25*time.Millisecond)
	tracker.RecordFuncExec("F", time.Now(), 25*time.Millisecond)
	tracker.RecordEdge("C", "G", 25*time.Millisecond)
	tracker.RecordFuncExec("G", time.Now(), 25*time.Millisecond)

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

// TestRecordCallWithSelfLoop tests RecordCall with self-loop
func TestRecordCallWithSelfLoop(t *testing.T) {
	tracker := New()

	// RecordCall should also prevent self-loops
	tracker.RecordEdgeCall("funcA", "funcA")

	assert.Equal(t, 0, tracker.EdgeCount())

	// Normal call should work
	tracker.RecordEdgeCall("funcA", "funcB")
	assert.Equal(t, 1, tracker.EdgeCount())
}

// TestNegativeExecutionTime tests handling of negative execution times
func TestNegativeExecutionTime(t *testing.T) {
	tracker := New()

	// Negative execution time should still be recorded (no validation)
	tracker.RecordFuncExec("funcA", time.Now(), -100*time.Millisecond)

	stats, ok := tracker.GetFunctionStats("funcA")
	require.True(t, ok, "function stats not found")

	// The negative value should be recorded as-is
	assert.Equal(t, -100*time.Millisecond, stats.TotalExecutionTime)
}

// TestRecordColdStart tests cold start tracking
func TestRecordColdStart(t *testing.T) {
	tracker := New()
	now := time.Now()

	// Create function stats first
	tracker.RecordFuncExec("funcA", now, 100*time.Millisecond)
	// Record a cold start
	tracker.RecordColdStart("funcA", now, 500*time.Millisecond)

	// Check cold start stats
	coldStarts, lastColdStartAt, lastColdStartDuration, ok := tracker.GetColdStartStats("funcA")
	require.True(t, ok, "function stats not found")
	assert.Equal(t, 1, coldStarts)
	assert.Equal(t, now, lastColdStartAt)
	assert.Equal(t, 500*time.Millisecond, lastColdStartDuration)

	// Record another cold start
	now2 := now.Add(1 * time.Minute)
	tracker.RecordColdStart("funcA", now2, 600*time.Millisecond)

	coldStarts, lastColdStartAt, lastColdStartDuration, ok = tracker.GetColdStartStats("funcA")
	require.True(t, ok)
	assert.Equal(t, 2, coldStarts)
	assert.Equal(t, now2, lastColdStartAt)
	assert.Equal(t, 600*time.Millisecond, lastColdStartDuration)
}

// TestGetColdStartAverage tests cold start average calculation
func TestGetColdStartAverage(t *testing.T) {
	tracker := New()
	now := time.Now()

	// Create function stats first
	tracker.RecordFuncExec("funcA", now, 100*time.Millisecond)
	// No cold starts yet
	avg := tracker.GetColdStartAverage("funcA")
	assert.Equal(t, time.Duration(0), avg)

	// Record cold starts with different durations
	tracker.RecordColdStart("funcA", now, 500*time.Millisecond)
	tracker.RecordColdStart("funcA", now, 600*time.Millisecond)
	tracker.RecordColdStart("funcA", now, 700*time.Millisecond)

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
	tracker := New()

	// Try to record cold start with empty function name
	tracker.RecordColdStart("", time.Now(), 500*time.Millisecond)

	// Should not create any stats
	assert.Equal(t, 0, tracker.FunctionCount())
}

// TestColdStartInFunctionStats tests that cold start info appears in function stats
func TestColdStartInFunctionStats(t *testing.T) {
	tracker := New()
	now := time.Now()

	// Record function executions and cold starts
	tracker.RecordFuncExec("funcA", now, 100*time.Millisecond)
	tracker.RecordColdStart("funcA", now, 500*time.Millisecond)
	tracker.RecordFuncExec("funcA", now, 150*time.Millisecond)

	stats, ok := tracker.GetFunctionStats("funcA")
	require.True(t, ok)

	assert.Equal(t, 2, int(stats.TotalCalls))
	assert.Equal(t, 1, stats.TotalColdStarts)
	assert.Equal(t, now, stats.LastColdStartAt)
	assert.Equal(t, 500*time.Millisecond, stats.LastColdStartDuration)
}
