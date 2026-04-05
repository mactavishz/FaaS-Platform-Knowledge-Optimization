package integration

import (
	"encoding/json"
	"testing"
	"time"
)

func TestIntegration_NoAutoscalerNoCallgraph(t *testing.T) {
	t.Log("[scenario] no autoscaler + no callgraph")
	requireTinyFaaSVM(t)
	rebuildTinyFaaS(t, "no-autoscaler-no-callgraph.env")
	waitForGateway(t, defaultGatewayURL, 90*time.Second)

	wipeFunctions(t)
	t.Cleanup(func() { wipeFunctions(t) })

	deployLinear2Workflow(t)
	waitForFunctionsReady(t, []string{"linear2-a", "linear2-b"}, true, 60*time.Second)

	body := invokeFunctionEventually(t, "linear2-a", 90*time.Second)
	assertLinear2Response(t, body)

	waitForFunctionsReady(t, []string{"linear2-a", "linear2-b"}, true, 15*time.Second)
	// Idle timeout is 12s in profile. With autoscaler disabled, these should stay running.
	time.Sleep(20 * time.Second)
	waitForFunctionsReady(t, []string{"linear2-a", "linear2-b"}, true, 15*time.Second)

	assertCallgraphEmpty(t)
}

func TestIntegration_AutoscalerOnly(t *testing.T) {
	t.Log("[scenario] autoscaler only")
	requireTinyFaaSVM(t)
	rebuildTinyFaaS(t, "autoscaler-only.env")
	waitForGateway(t, defaultGatewayURL, 90*time.Second)

	wipeFunctions(t)
	t.Cleanup(func() { wipeFunctions(t) })

	deployLinear2Workflow(t)
	waitForFunctionsReady(t, []string{"linear2-a", "linear2-b"}, true, 60*time.Second)
	body := invokeFunctionEventually(t, "linear2-a", 90*time.Second)
	assertLinear2Response(t, body)

	// Idle timeout is 12s + autoscaler check interval is 10s.
	waitForFunctionsReady(t, []string{"linear2-a", "linear2-b"}, false, 90*time.Second)

	body = invokeFunctionEventually(t, "linear2-a", 120*time.Second)
	assertLinear2Response(t, body)

	assertCallgraphEmpty(t)
}

func TestIntegration_AutoscalerAndCallgraph(t *testing.T) {
	t.Log("[scenario] autoscaler + callgraph")
	requireTinyFaaSVM(t)
	rebuildTinyFaaS(t, "autoscaler-and-callgraph.env")
	waitForGateway(t, defaultGatewayURL, 90*time.Second)

	wipeFunctions(t)
	t.Cleanup(func() { wipeFunctions(t) })

	deployLinear2Workflow(t)
	waitForFunctionsReady(t, []string{"linear2-a", "linear2-b"}, true, 60*time.Second)

	for i := 0; i < 3; i++ {
		body := invokeFunctionEventually(t, "linear2-a", 90*time.Second)
		assertLinear2Response(t, body)
	}

	cg := getCallGraph(t)
	initialCount := edgeCount(cg, "", "linear2-a")
	if initialCount < 3 {
		t.Fatalf("expected callgraph edge external->linear2-a count >= 3, got %d", initialCount)
	}

	waitForFunctionsReady(t, []string{"linear2-a", "linear2-b"}, false, 90*time.Second)

	body := invokeFunctionEventually(t, "linear2-a", 120*time.Second)
	assertLinear2Response(t, body)

	cg = getCallGraph(t)
	updatedCount := edgeCount(cg, "", "linear2-a")
	if updatedCount <= initialCount {
		t.Fatalf("expected callgraph edge count to increase after re-invocation, before=%d after=%d", initialCount, updatedCount)
	}
}

func assertCallgraphEmpty(t *testing.T) {
	t.Helper()

	cg := getCallGraph(t)
	if len(cg.Edges) != 0 {
		t.Fatalf("expected empty callgraph edges when disabled, got %d", len(cg.Edges))
	}
	if len(cg.Functions) != 0 {
		t.Fatalf("expected empty callgraph functions when disabled, got %d", len(cg.Functions))
	}
}

func assertLinear2Response(t *testing.T, body []byte) {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("expected JSON response from linear2 workflow, got: %s", string(body))
	}
	if payload["msg"] == nil {
		t.Fatalf("expected workflow response to contain msg, got: %s", string(body))
	}
}
