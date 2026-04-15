package tinyfaas

import (
	"encoding/json"
	"testing"
	"time"

	integrationhelpers "github.com/mactavishz/FaaS-Platform-Knowledge-Optimization/tests/integration/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type PlatformIntegrationSuite struct {
	suite.Suite
}

func TestPlatformIntegrationSuite(t *testing.T) {
	suite.Run(t, new(PlatformIntegrationSuite))
}

func (s *PlatformIntegrationSuite) setupScenario(envProfile string) {
	t := s.T()
	t.Logf("[setup] profile=%s", envProfile)
	integrationhelpers.RequireTinyFaaSVM(t)
	integrationhelpers.RebuildTinyFaaS(t, envProfile)
	integrationhelpers.WaitForGateway(t, integrationhelpers.DefaultGatewayURL, 90*time.Second)
	integrationhelpers.WipeFunctions(t)
	t.Cleanup(func() { integrationhelpers.WipeFunctions(t) })
	integrationhelpers.DeployWorkflow(t, integrationhelpers.Linear3StackPath)
	integrationhelpers.WaitForFunctionsReady(t, []string{"linear3-a", "linear3-b", "linear3-c"}, true, 60*time.Second)
}

func (s *PlatformIntegrationSuite) TestNoAutoscalerNoCallgraph() {
	t := s.T()
	t.Log("[scenario] no autoscaler + no callgraph")
	s.setupScenario("no-autoscaler-no-callgraph.env")

	body := integrationhelpers.InvokeFunctionEventually(t, "linear3-a", 90*time.Second)
	s.assertWorkflowResponse(body)

	integrationhelpers.WaitForFunctionsReady(t, []string{"linear3-a", "linear3-b", "linear3-c"}, true, 15*time.Second)
	// Idle timeout is 12s in profile. With autoscaler disabled, these should stay running.
	time.Sleep(20 * time.Second)
	integrationhelpers.WaitForFunctionsReady(t, []string{"linear3-a", "linear3-b", "linear3-c"}, true, 15*time.Second)

	s.assertCallgraphEmpty()
}

func (s *PlatformIntegrationSuite) TestAutoscalerOnly() {
	t := s.T()
	t.Log("[scenario] autoscaler only")
	s.setupScenario("autoscaler-only.env")

	body := integrationhelpers.InvokeFunctionEventually(t, "linear3-a", 90*time.Second)
	s.assertWorkflowResponse(body)

	// Idle timeout is 12s + autoscaler check interval is 10s.
	integrationhelpers.WaitForFunctionsReady(t, []string{"linear3-a", "linear3-b", "linear3-c"}, false, 90*time.Second)

	body = integrationhelpers.InvokeFunctionEventually(t, "linear3-a", 120*time.Second)
	s.assertWorkflowResponse(body)

	s.assertCallgraphEmpty()
}

func (s *PlatformIntegrationSuite) TestAutoscalerAndCallgraph() {
	t := s.T()
	t.Log("[scenario] autoscaler + callgraph")
	s.setupScenario("autoscaler-and-callgraph.env")

	body := integrationhelpers.InvokeFunctionEventually(t, "linear3-a", 90*time.Second)
	s.assertWorkflowResponse(body)

	cg := integrationhelpers.GetCallGraph(t)
	count := integrationhelpers.EdgeCount(cg, "", "linear3-a")
	assert.Equal(t, 1, count, "expected callgraph edge external->linear3-a count = 1, got %d", count)

	countAB := integrationhelpers.EdgeCount(cg, "linear3-a", "linear3-b")
	countBC := integrationhelpers.EdgeCount(cg, "linear3-b", "linear3-c")
	assert.Equal(t, 1, countAB, "expected callgraph edge linear3-a->linear3-b count = 1, got %d", countAB)
	assert.Equal(t, 1, countBC, "expected callgraph edge linear3-b->linear3-c count = 1, got %d", countBC)

	integrationhelpers.WaitForFunctionsReady(t, []string{"linear3-a", "linear3-b", "linear3-c"}, false, 90*time.Second)

	body = integrationhelpers.InvokeFunctionEventually(t, "linear3-a", 120*time.Second)
	s.assertWorkflowResponse(body)

	cg = integrationhelpers.GetCallGraph(t)
	updatedCount := integrationhelpers.EdgeCount(cg, "", "linear3-a")
	assert.Equal(t, 2, updatedCount, "expected callgraph edge external->linear3-a count to increase after re-invocation, before=%d after=%d", count, updatedCount)

	updatedCountAB := integrationhelpers.EdgeCount(cg, "linear3-a", "linear3-b")
	updatedCountBC := integrationhelpers.EdgeCount(cg, "linear3-b", "linear3-c")
	assert.Equal(t, 2, updatedCountAB, "expected callgraph edge linear3-a->linear3-b count to increase after re-invocation, before=%d after=%d", countAB, updatedCountAB)
	assert.Equal(t, 2, updatedCountBC, "expected callgraph edge linear3-b->linear3-c count to increase after re-invocation, before=%d after=%d", countBC, updatedCountBC)
}

func (s *PlatformIntegrationSuite) assertCallgraphEmpty() {
	t := s.T()
	t.Helper()

	cg := integrationhelpers.GetCallGraph(t)
	assert.Empty(t, cg.Edges, "expected empty callgraph edges when disabled")
	assert.Empty(t, cg.Functions, "expected empty callgraph functions when disabled")
}

func (s *PlatformIntegrationSuite) assertWorkflowResponse(body []byte) {
	t := s.T()
	t.Helper()

	var payload map[string]any
	err := json.Unmarshal(body, &payload)
	require.NoError(t, err, "expected JSON response from workflow, got: %s", string(body))
	assert.NotNil(t, payload["msg"], "expected workflow response to contain msg, got: %s", string(body))
}
