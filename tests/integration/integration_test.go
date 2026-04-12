package integration

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type IntegrationSuite struct {
	suite.Suite
}

func TestIntegrationSuite(t *testing.T) {
	suite.Run(t, new(IntegrationSuite))
}

func (s *IntegrationSuite) setupScenario(envProfile string) {
	t := s.T()
	t.Logf("[setup] profile=%s", envProfile)
	requireTinyFaaSVM(t)
	rebuildTinyFaaS(t, envProfile)
	waitForGateway(t, defaultGatewayURL, 90*time.Second)
	wipeFunctions(t)
	t.Cleanup(func() { wipeFunctions(t) })
	deployLinear3Workflow(t)
	waitForFunctionsReady(t, []string{"linear3-a", "linear3-b", "linear3-c"}, true, 60*time.Second)
}

func (s *IntegrationSuite) TestNoAutoscalerNoCallgraph() {
	t := s.T()
	t.Log("[scenario] no autoscaler + no callgraph")
	s.setupScenario("no-autoscaler-no-callgraph.env")

	body := invokeFunctionEventually(t, "linear3-a", 90*time.Second)
	s.assertWorkflowResponse(body)

	waitForFunctionsReady(t, []string{"linear3-a", "linear3-b", "linear3-c"}, true, 15*time.Second)
	// Idle timeout is 12s in profile. With autoscaler disabled, these should stay running.
	time.Sleep(20 * time.Second)
	waitForFunctionsReady(t, []string{"linear3-a", "linear3-b", "linear3-c"}, true, 15*time.Second)

	s.assertCallgraphEmpty()
}

func (s *IntegrationSuite) TestAutoscalerOnly() {
	t := s.T()
	t.Log("[scenario] autoscaler only")
	s.setupScenario("autoscaler-only.env")

	body := invokeFunctionEventually(t, "linear3-a", 90*time.Second)
	s.assertWorkflowResponse(body)

	// Idle timeout is 12s + autoscaler check interval is 10s.
	waitForFunctionsReady(t, []string{"linear3-a", "linear3-b", "linear3-c"}, false, 90*time.Second)

	body = invokeFunctionEventually(t, "linear3-a", 120*time.Second)
	s.assertWorkflowResponse(body)

	s.assertCallgraphEmpty()
}

func (s *IntegrationSuite) TestAutoscalerAndCallgraph() {
	t := s.T()
	t.Log("[scenario] autoscaler + callgraph")
	s.setupScenario("autoscaler-and-callgraph.env")

	body := invokeFunctionEventually(t, "linear3-a", 90*time.Second)
	s.assertWorkflowResponse(body)

	cg := getCallGraph(t)
	count := edgeCount(cg, "", "linear3-a")
	assert.Equal(t, 1, count, "expected callgraph edge external->linear3-a count = 1, got %d", count)

	countAB := edgeCount(cg, "linear3-a", "linear3-b")
	countBC := edgeCount(cg, "linear3-b", "linear3-c")
	assert.Equal(t, 1, countAB, "expected callgraph edge linear3-a->linear3-b count = 1, got %d", countAB)
	assert.Equal(t, 1, countBC, "expected callgraph edge linear3-b->linear3-c count = 1, got %d", countBC)

	waitForFunctionsReady(t, []string{"linear3-a", "linear3-b", "linear3-c"}, false, 90*time.Second)

	body = invokeFunctionEventually(t, "linear3-a", 120*time.Second)
	s.assertWorkflowResponse(body)

	cg = getCallGraph(t)
	updatedCount := edgeCount(cg, "", "linear3-a")
	assert.Equal(t, 2, updatedCount, "expected callgraph edge external->linear3-a count to increase after re-invocation, before=%d after=%d", count, updatedCount)

	updatedCountAB := edgeCount(cg, "linear3-a", "linear3-b")
	updatedCountBC := edgeCount(cg, "linear3-b", "linear3-c")
	assert.Equal(t, 2, updatedCountAB, "expected callgraph edge linear3-a->linear3-b count to increase after re-invocation, before=%d after=%d", countAB, updatedCountAB)
	assert.Equal(t, 2, updatedCountBC, "expected callgraph edge linear3-b->linear3-c count to increase after re-invocation, before=%d after=%d", countBC, updatedCountBC)
}

func (s *IntegrationSuite) assertCallgraphEmpty() {
	t := s.T()
	t.Helper()

	cg := getCallGraph(t)
	assert.Empty(t, cg.Edges, "expected empty callgraph edges when disabled")
	assert.Empty(t, cg.Functions, "expected empty callgraph functions when disabled")
}

func (s *IntegrationSuite) assertWorkflowResponse(body []byte) {
	t := s.T()
	t.Helper()

	var payload map[string]any
	err := json.Unmarshal(body, &payload)
	require.NoError(t, err, "expected JSON response from workflow, got: %s", string(body))
	assert.NotNil(t, payload["msg"], "expected workflow response to contain msg, got: %s", string(body))
}
