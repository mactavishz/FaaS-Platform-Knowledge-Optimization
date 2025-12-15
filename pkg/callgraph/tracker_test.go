package callgraph

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewTracker(t *testing.T) {
	tracker := New()
	if tracker == nil {
		t.Fatal("New() returned nil")
	}
	if tracker.EdgeCount() != 0 {
		t.Errorf("expected 0 edges, got %d", tracker.EdgeCount())
	}
	if tracker.FunctionCount() != 0 {
		t.Errorf("expected 0 functions, got %d", tracker.FunctionCount())
	}
}

func TestNewTrackerWithConfig(t *testing.T) {
	config := Config{
		AveragingMethod: ExponentialMovingAverage,
		EMAAlpha:        0.5,
		MaxEdges:        100,
	}
	tracker := NewWithConfig(config)
	if tracker == nil {
		t.Fatal("NewWithConfig() returned nil")
	}
	if tracker.config.AveragingMethod != ExponentialMovingAverage {
		t.Errorf("expected EMA, got %v", tracker.config.AveragingMethod)
	}
}

func TestRecord(t *testing.T) {
	tracker := New()

	// Record a call
	tracker.Record("funcA", "funcB", int64(100*time.Millisecond))

	// Check edge count
	if tracker.EdgeCount() != 1 {
		t.Errorf("expected 1 edge, got %d", tracker.EdgeCount())
	}

	// Check function count
	if tracker.FunctionCount() != 1 {
		t.Errorf("expected 1 function (callee), got %d", tracker.FunctionCount())
	}

	// Check callees
	callees := tracker.GetCallees("funcA")
	if len(callees) != 1 || callees[0] != "funcB" {
		t.Errorf("expected [funcB], got %v", callees)
	}

	// Check callers
	callers := tracker.GetCallers("funcB")
	if len(callers) != 1 || callers[0] != "funcA" {
		t.Errorf("expected [funcA], got %v", callers)
	}
}

func TestRecordExternalCall(t *testing.T) {
	tracker := New()

	// Record an external call (empty caller)
	tracker.Record("", "funcA", int64(50*time.Millisecond))

	// Check that external calls are tracked
	callees := tracker.GetCallees("")
	if len(callees) != 1 || callees[0] != "funcA" {
		t.Errorf("expected external call to funcA, got %v", callees)
	}
}

func TestRecordCall(t *testing.T) {
	tracker := New()

	// Record a call without execution time
	tracker.RecordCall("funcA", "funcB")

	if tracker.EdgeCount() != 1 {
		t.Errorf("expected 1 edge, got %d", tracker.EdgeCount())
	}

	// Verify execution time is 0
	stats, ok := tracker.GetFunctionStats("funcB")
	if !ok {
		t.Fatal("function stats not found")
	}
	if stats.AvgExecutionTime != 0 {
		t.Errorf("expected 0 avg execution time, got %v", stats.AvgExecutionTime)
	}
}

func TestGetFunctionStats(t *testing.T) {
	tracker := New()

	// Record multiple calls
	tracker.Record("", "funcA", int64(100*time.Millisecond))
	tracker.Record("", "funcA", int64(200*time.Millisecond))
	tracker.Record("", "funcA", int64(150*time.Millisecond))

	stats, ok := tracker.GetFunctionStats("funcA")
	if !ok {
		t.Fatal("function stats not found")
	}

	if stats.TotalCalls != 3 {
		t.Errorf("expected 3 total calls, got %d", stats.TotalCalls)
	}

	expectedTotal := 450 * time.Millisecond
	if stats.TotalExecutionTime != expectedTotal {
		t.Errorf("expected total %v, got %v", expectedTotal, stats.TotalExecutionTime)
	}

	if stats.MinExecutionTime != 100*time.Millisecond {
		t.Errorf("expected min 100ms, got %v", stats.MinExecutionTime)
	}

	if stats.MaxExecutionTime != 200*time.Millisecond {
		t.Errorf("expected max 200ms, got %v", stats.MaxExecutionTime)
	}
}

func TestGetEdgeStats(t *testing.T) {
	tracker := New()

	tracker.Record("funcA", "funcB", int64(100*time.Millisecond))
	tracker.Record("funcA", "funcB", int64(200*time.Millisecond))

	stats, ok := tracker.GetEdgeStats("funcA", "funcB")
	if !ok {
		t.Fatal("edge stats not found")
	}

	if stats.Count != 2 {
		t.Errorf("expected count 2, got %d", stats.Count)
	}

	if stats.TotalExecutionTime != 300*time.Millisecond {
		t.Errorf("expected total 300ms, got %v", stats.TotalExecutionTime)
	}
}

func TestGetCallGraph(t *testing.T) {
	tracker := New()

	tracker.Record("", "funcA", int64(50*time.Millisecond))
	tracker.Record("funcA", "funcB", int64(100*time.Millisecond))
	tracker.Record("funcA", "funcC", int64(150*time.Millisecond))

	graph := tracker.GetCallGraph()

	if len(graph.Edges) != 3 {
		t.Errorf("expected 3 edges, got %d", len(graph.Edges))
	}

	if len(graph.Functions) != 3 {
		t.Errorf("expected 3 functions, got %d", len(graph.Functions))
	}

	if graph.TotalCalls != 3 {
		t.Errorf("expected 3 total calls, got %d", graph.TotalCalls)
	}
}

func TestMaxEdgesLimit(t *testing.T) {
	config := DefaultConfig()
	config.MaxEdges = 5
	tracker := NewWithConfig(config)

	// Record more edges than the limit
	for i := 0; i < 10; i++ {
		tracker.Record("funcA", "funcB", int64(time.Millisecond))
	}

	if tracker.EdgeCount() != 5 {
		t.Errorf("expected 5 edges (limited), got %d", tracker.EdgeCount())
	}
}

func TestClear(t *testing.T) {
	tracker := New()

	tracker.Record("funcA", "funcB", int64(100*time.Millisecond))
	tracker.Record("funcB", "funcC", int64(200*time.Millisecond))

	tracker.Clear()

	if tracker.EdgeCount() != 0 {
		t.Errorf("expected 0 edges after clear, got %d", tracker.EdgeCount())
	}

	if tracker.FunctionCount() != 0 {
		t.Errorf("expected 0 functions after clear, got %d", tracker.FunctionCount())
	}
}

func TestToJSON(t *testing.T) {
	tracker := New()

	tracker.Record("", "funcA", int64(100*time.Millisecond))
	tracker.Record("funcA", "funcB", int64(200*time.Millisecond))

	data, err := tracker.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	var graph CallGraph
	if err := json.Unmarshal(data, &graph); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if len(graph.Edges) != 2 {
		t.Errorf("expected 2 edges in JSON, got %d", len(graph.Edges))
	}
}

func TestFromJSON(t *testing.T) {
	tracker := New()

	tracker.Record("", "funcA", int64(100*time.Millisecond))
	tracker.Record("funcA", "funcB", int64(200*time.Millisecond))

	data, err := tracker.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	// Create new tracker and load from JSON
	newTracker := New()
	if err := newTracker.FromJSON(data); err != nil {
		t.Fatalf("FromJSON failed: %v", err)
	}

	// Verify data was loaded
	if newTracker.UniqueEdgeCount() != 2 {
		t.Errorf("expected 2 unique edges, got %d", newTracker.UniqueEdgeCount())
	}

	if newTracker.FunctionCount() != 2 {
		t.Errorf("expected 2 functions, got %d", newTracker.FunctionCount())
	}
}

func TestGetCallPaths(t *testing.T) {
	tracker := New()

	// Create a workflow: external -> A -> B -> C
	tracker.Record("", "funcA", int64(100*time.Millisecond))
	tracker.Record("funcA", "funcB", int64(200*time.Millisecond))
	tracker.Record("funcB", "funcC", int64(150*time.Millisecond))

	paths := tracker.GetCallPaths()

	if len(paths) != 1 {
		t.Errorf("expected 1 path, got %d", len(paths))
	}

	if len(paths) > 0 {
		if len(paths[0].Path) != 3 {
			t.Errorf("expected path length 3, got %d", len(paths[0].Path))
		}
		expectedPath := []string{"funcA", "funcB", "funcC"}
		for i, fn := range paths[0].Path {
			if fn != expectedPath[i] {
				t.Errorf("expected %s at position %d, got %s", expectedPath[i], i, fn)
			}
		}
	}
}

func TestGetPathsContaining(t *testing.T) {
	tracker := New()

	tracker.Record("", "funcA", int64(100*time.Millisecond))
	tracker.Record("funcA", "funcB", int64(200*time.Millisecond))
	tracker.Record("funcA", "funcC", int64(150*time.Millisecond))

	paths := tracker.GetPathsContaining("funcB")

	if len(paths) != 1 {
		t.Errorf("expected 1 path containing funcB, got %d", len(paths))
	}
}

func TestGetLongestPath(t *testing.T) {
	tracker := New()

	// Path 1: A -> B (length 2)
	tracker.Record("", "funcA", int64(100*time.Millisecond))
	tracker.Record("funcA", "funcB", int64(200*time.Millisecond))

	// Path 2: X -> Y -> Z (length 3) - separate entry point
	tracker.Record("", "funcX", int64(100*time.Millisecond))
	tracker.Record("funcX", "funcY", int64(200*time.Millisecond))
	tracker.Record("funcY", "funcZ", int64(150*time.Millisecond))

	longest := tracker.GetLongestPath()

	if len(longest.Path) != 3 {
		t.Errorf("expected longest path length 3, got %d", len(longest.Path))
	}
}

func TestGetSlowestPath(t *testing.T) {
	tracker := New()

	// Fast path
	tracker.Record("", "fastA", int64(10*time.Millisecond))
	tracker.Record("fastA", "fastB", int64(10*time.Millisecond))

	// Slow path
	tracker.Record("", "slowA", int64(500*time.Millisecond))
	tracker.Record("slowA", "slowB", int64(500*time.Millisecond))

	slowest := tracker.GetSlowestPath()

	if len(slowest.Path) > 0 && slowest.Path[0] != "slowA" {
		t.Errorf("expected slowest path to start with slowA, got %v", slowest.Path)
	}
}

func TestGetAverageExecutionTime(t *testing.T) {
	tracker := New()

	tracker.Record("", "funcA", int64(100*time.Millisecond))
	tracker.Record("", "funcA", int64(200*time.Millisecond))
	tracker.Record("", "funcA", int64(300*time.Millisecond))

	avg := tracker.GetAverageExecutionTime("funcA")

	// SMA with 3 samples: (100+200+300)/3 = 200ms
	expected := int64(200 * time.Millisecond)
	if avg != expected {
		t.Errorf("expected avg %d, got %d", expected, avg)
	}
}

func TestConcurrentAccess(t *testing.T) {
	tracker := New()
	done := make(chan bool)

	// Concurrent writers
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				tracker.Record("funcA", "funcB", int64(time.Millisecond))
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

	// Should have recorded 1000 edges (10 writers * 100 each)
	stats, ok := tracker.GetFunctionStats("funcB")
	if !ok {
		t.Fatal("function stats not found")
	}
	if stats.TotalCalls != 1000 {
		t.Errorf("expected 1000 total calls, got %d", stats.TotalCalls)
	}
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
	tracker.Record("", "A", int64(100*time.Millisecond))
	tracker.Record("A", "B", int64(50*time.Millisecond))
	tracker.Record("A", "C", int64(75*time.Millisecond))

	paths := tracker.GetCallPaths()

	// Should have 2 paths: A->B and A->C
	if len(paths) != 2 {
		t.Errorf("expected 2 paths for branching DAG, got %d", len(paths))
	}

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
	tracker.Record("", "A", int64(100*time.Millisecond))
	tracker.Record("A", "B", int64(50*time.Millisecond))
	tracker.Record("A", "C", int64(75*time.Millisecond))
	tracker.Record("B", "D", int64(25*time.Millisecond))
	tracker.Record("C", "D", int64(30*time.Millisecond))

	paths := tracker.GetCallPaths()

	// Should have 2 paths: A->B->D and A->C->D
	if len(paths) != 2 {
		t.Errorf("expected 2 paths for diamond DAG, got %d", len(paths))
	}

	// Both paths should end with D
	for _, path := range paths {
		if len(path.Path) != 3 {
			t.Errorf("expected path length 3, got %d: %v", len(path.Path), path.Path)
		}
		if path.Path[len(path.Path)-1] != "D" {
			t.Errorf("expected path to end with D, got %v", path.Path)
		}
	}
}

// TestDAGWithMultipleEntryPoints tests a DAG with multiple entry points
func TestDAGWithMultipleEntryPoints(t *testing.T) {
	tracker := New()

	// Two entry points: X and Y, both eventually reach Z
	tracker.Record("", "X", int64(100*time.Millisecond))
	tracker.Record("", "Y", int64(100*time.Millisecond))
	tracker.Record("X", "Z", int64(50*time.Millisecond))
	tracker.Record("Y", "Z", int64(50*time.Millisecond))

	paths := tracker.GetCallPaths()

	// Should have 2 paths: X->Z and Y->Z
	if len(paths) != 2 {
		t.Errorf("expected 2 paths for multiple entry points, got %d", len(paths))
	}

	entryPoints := tracker.GetEntryPoints()
	if len(entryPoints) != 2 {
		t.Errorf("expected 2 entry points, got %d", len(entryPoints))
	}
}

// TestDAGComplexTopology tests a more complex DAG
// Graph:     A -> B -> D -> F
//
//	A -> C -> E -> F
//	A -> C -> E -> G
func TestDAGComplexTopology(t *testing.T) {
	tracker := New()

	tracker.Record("", "A", int64(100*time.Millisecond))
	tracker.Record("A", "B", int64(50*time.Millisecond))
	tracker.Record("A", "C", int64(60*time.Millisecond))
	tracker.Record("B", "D", int64(40*time.Millisecond))
	tracker.Record("C", "E", int64(45*time.Millisecond))
	tracker.Record("D", "F", int64(30*time.Millisecond))
	tracker.Record("E", "F", int64(35*time.Millisecond))
	tracker.Record("E", "G", int64(25*time.Millisecond))

	paths := tracker.GetCallPaths()

	// Should have 3 paths:
	// A->B->D->F
	// A->C->E->F
	// A->C->E->G
	if len(paths) != 3 {
		t.Errorf("expected 3 paths for complex DAG, got %d", len(paths))
		for i, p := range paths {
			t.Logf("Path %d: %v", i, p.Path)
		}
	}

	// Check leaf functions
	leaves := tracker.GetLeafFunctions()
	if len(leaves) != 2 {
		t.Errorf("expected 2 leaf functions (F and G), got %d: %v", len(leaves), leaves)
	}
}

func TestGetEntryPoints(t *testing.T) {
	tracker := New()

	tracker.Record("", "A", int64(100*time.Millisecond))
	tracker.Record("", "B", int64(100*time.Millisecond))
	tracker.Record("A", "C", int64(50*time.Millisecond))
	tracker.Record("B", "C", int64(50*time.Millisecond))

	entryPoints := tracker.GetEntryPoints()

	if len(entryPoints) != 2 {
		t.Errorf("expected 2 entry points, got %d", len(entryPoints))
	}
}

func TestGetLeafFunctions(t *testing.T) {
	tracker := New()

	tracker.Record("", "A", int64(100*time.Millisecond))
	tracker.Record("A", "B", int64(50*time.Millisecond))
	tracker.Record("A", "C", int64(75*time.Millisecond))
	// B and C are leaves (they don't call anything)

	leaves := tracker.GetLeafFunctions()

	if len(leaves) != 2 {
		t.Errorf("expected 2 leaf functions, got %d: %v", len(leaves), leaves)
	}
}

func TestGetDownstreamFunctions(t *testing.T) {
	tracker := New()

	// A -> B -> D
	// A -> C -> D
	tracker.Record("", "A", int64(100*time.Millisecond))
	tracker.Record("A", "B", int64(50*time.Millisecond))
	tracker.Record("A", "C", int64(75*time.Millisecond))
	tracker.Record("B", "D", int64(25*time.Millisecond))
	tracker.Record("C", "D", int64(30*time.Millisecond))

	downstream := tracker.GetDownstreamFunctions("A")

	// A can reach B, C, D
	if len(downstream) != 3 {
		t.Errorf("expected 3 downstream functions from A, got %d: %v", len(downstream), downstream)
	}

	downstreamFromB := tracker.GetDownstreamFunctions("B")
	if len(downstreamFromB) != 1 || downstreamFromB[0] != "D" {
		t.Errorf("expected [D] downstream from B, got %v", downstreamFromB)
	}
}

func TestGetUpstreamFunctions(t *testing.T) {
	tracker := New()

	// A -> B -> D
	// A -> C -> D
	tracker.Record("", "A", int64(100*time.Millisecond))
	tracker.Record("A", "B", int64(50*time.Millisecond))
	tracker.Record("A", "C", int64(75*time.Millisecond))
	tracker.Record("B", "D", int64(25*time.Millisecond))
	tracker.Record("C", "D", int64(30*time.Millisecond))

	upstream := tracker.GetUpstreamFunctions("D")

	// D can be reached from A, B, C
	if len(upstream) != 3 {
		t.Errorf("expected 3 upstream functions to D, got %d: %v", len(upstream), upstream)
	}

	upstreamToA := tracker.GetUpstreamFunctions("A")
	if len(upstreamToA) != 0 {
		t.Errorf("expected no upstream functions to A (entry point), got %v", upstreamToA)
	}
}

func TestGetSubgraph(t *testing.T) {
	tracker := New()

	tracker.Record("", "A", int64(100*time.Millisecond))
	tracker.Record("A", "B", int64(50*time.Millisecond))
	tracker.Record("A", "C", int64(75*time.Millisecond))
	tracker.Record("B", "D", int64(25*time.Millisecond))

	// Get subgraph with only A, B, D
	subgraph := tracker.GetSubgraph([]string{"A", "B", "D"})

	// Should have 3 edges: ""->A (external), A->B, and B->D (A->C excluded because C is not in the list)
	if len(subgraph.Edges) != 3 {
		t.Errorf("expected 3 edges in subgraph (including external entry), got %d", len(subgraph.Edges))
		for _, e := range subgraph.Edges {
			t.Logf("Edge: %s -> %s", e.Caller, e.Callee)
		}
	}

	if len(subgraph.Functions) != 3 {
		t.Errorf("expected 3 functions in subgraph, got %d", len(subgraph.Functions))
	}
}
