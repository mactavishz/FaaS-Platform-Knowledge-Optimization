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

func (s *PlatformIntegrationSuite) setupScenario(envProfile string, stackPath string, deployEnvs map[string]string) {
	t := s.T()
	t.Logf("[setup] profile=%s stack=%s", envProfile, stackPath)
	integrationhelpers.RequireTinyFaaSVM(t)
	integrationhelpers.RebuildTinyFaaS(t, envProfile)
	integrationhelpers.WaitForGateway(t, integrationhelpers.DefaultGatewayURL, 90*time.Second)
	integrationhelpers.WipeFunctions(t)
	t.Cleanup(func() { integrationhelpers.WipeFunctions(t) })
	integrationhelpers.DeployWorkflowWithEnvs(t, stackPath, deployEnvs)
	integrationhelpers.WaitForFunctionsPresent(t, []string{"linear3-a", "linear3-b", "linear3-c"}, 60*time.Second)
}

func (s *PlatformIntegrationSuite) TestNoAutoscalerNoCallgraph() {
	t := s.T()
	t.Log("[scenario] no autoscaler + no callgraph")
	s.setupScenario("no-autoscaler-no-callgraph.env", integrationhelpers.Linear3StackPath, nil)

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
	s.setupScenario("autoscaler-only.env", integrationhelpers.Linear3StackPath, nil)

	body := integrationhelpers.InvokeFunctionEventually(t, "linear3-a", 90*time.Second)
	s.assertWorkflowResponse(body)

	// Idle timeout is 12s + autoscaler check interval is 10s.
	integrationhelpers.WaitForFunctionsReady(t, []string{"linear3-a", "linear3-b", "linear3-c"}, false, 90*time.Second)

	body = integrationhelpers.InvokeFunctionEventually(t, "linear3-a", 120*time.Second)
	s.assertWorkflowResponse(body)

	s.assertCallgraphEmpty()
}

func (s *PlatformIntegrationSuite) TestAutoscalerAndCallgraphNoPrewarm() {
	t := s.T()
	t.Log("[scenario] autoscaler + callgraph + prewarm disabled")
	s.setupScenario(
		"autoscaler-and-callgraph-no-prewarm.env",
		integrationhelpers.Linear3StackPath,
		map[string]string{"FUNCTION_DELAY_SEC": "5"},
	)

	body := integrationhelpers.InvokeFunctionEventually(t, "linear3-a", 90*time.Second)
	s.assertWorkflowResponse(body)

	cg := integrationhelpers.GetCallGraph(t)
	count := integrationhelpers.EdgeCount(cg, "", "linear3-a")
	assert.GreaterOrEqual(t, count, 1, "expected callgraph edge external->linear3-a count >= 1, got %d", count)

	countAB := integrationhelpers.EdgeCount(cg, "linear3-a", "linear3-b")
	countBC := integrationhelpers.EdgeCount(cg, "linear3-b", "linear3-c")
	assert.GreaterOrEqual(t, countAB, 1, "expected callgraph edge linear3-a->linear3-b count >= 1, got %d", countAB)
	assert.GreaterOrEqual(t, countBC, 1, "expected callgraph edge linear3-b->linear3-c count >= 1, got %d", countBC)

	statsBBefore := integrationhelpers.GetFunctionCallGraphStats(t, "linear3-b")
	statsCBefore := integrationhelpers.GetFunctionCallGraphStats(t, "linear3-c")

	integrationhelpers.WaitForFunctionsReady(t, []string{"linear3-a", "linear3-b", "linear3-c"}, false, 90*time.Second)

	body = integrationhelpers.InvokeFunctionEventually(t, "linear3-a", 180*time.Second)
	s.assertWorkflowResponse(body)

	cg = integrationhelpers.GetCallGraph(t)
	updatedCount := integrationhelpers.EdgeCount(cg, "", "linear3-a")
	assert.GreaterOrEqual(t, updatedCount, count+1, "expected callgraph edge external->linear3-a count to increase after re-invocation, before=%d after=%d", count, updatedCount)

	updatedCountAB := integrationhelpers.EdgeCount(cg, "linear3-a", "linear3-b")
	updatedCountBC := integrationhelpers.EdgeCount(cg, "linear3-b", "linear3-c")
	assert.GreaterOrEqual(t, updatedCountAB, countAB+1, "expected callgraph edge linear3-a->linear3-b count to increase after re-invocation, before=%d after=%d", countAB, updatedCountAB)
	assert.GreaterOrEqual(t, updatedCountBC, countBC+1, "expected callgraph edge linear3-b->linear3-c count to increase after re-invocation, before=%d after=%d", countBC, updatedCountBC)

	statsBAfter := integrationhelpers.GetFunctionCallGraphStats(t, "linear3-b")
	statsCAfter := integrationhelpers.GetFunctionCallGraphStats(t, "linear3-c")

	assert.Equal(t, statsBBefore.TotalPrewarms, statsBAfter.TotalPrewarms, "expected linear3-b prewarm count to stay unchanged when prewarming disabled, before=%d after=%d", statsBBefore.TotalPrewarms, statsBAfter.TotalPrewarms)
	assert.Equal(t, statsCBefore.TotalPrewarms, statsCAfter.TotalPrewarms, "expected linear3-c prewarm count to stay unchanged when prewarming disabled, before=%d after=%d", statsCBefore.TotalPrewarms, statsCAfter.TotalPrewarms)
	assert.GreaterOrEqual(t, statsBAfter.TotalColdStarts, statsBBefore.TotalColdStarts+1, "expected linear3-b cold starts to increase without prewarming, before=%d after=%d", statsBBefore.TotalColdStarts, statsBAfter.TotalColdStarts)
	assert.GreaterOrEqual(t, statsCAfter.TotalColdStarts, statsCBefore.TotalColdStarts+1, "expected linear3-c cold starts to increase without prewarming, before=%d after=%d", statsCBefore.TotalColdStarts, statsCAfter.TotalColdStarts)
}

func (s *PlatformIntegrationSuite) TestAutoscalerAndCallgraphAndPrewarm() {
	t := s.T()
	t.Log("[scenario] autoscaler + callgraph + prewarm enabled")
	s.setupScenario(
		"autoscaler-and-callgraph-and-prewarm.env",
		integrationhelpers.Linear3StackPath,
		map[string]string{"FUNCTION_DELAY_SEC": "5"},
	)

	body := integrationhelpers.InvokeFunctionEventually(t, "linear3-a", 90*time.Second)
	s.assertWorkflowResponse(body)

	cg := integrationhelpers.GetCallGraph(t)
	count := integrationhelpers.EdgeCount(cg, "", "linear3-a")
	assert.GreaterOrEqual(t, count, 1, "expected callgraph edge external->linear3-a count >= 1, got %d", count)

	countAB := integrationhelpers.EdgeCount(cg, "linear3-a", "linear3-b")
	countBC := integrationhelpers.EdgeCount(cg, "linear3-b", "linear3-c")
	assert.GreaterOrEqual(t, countAB, 1, "expected callgraph edge linear3-a->linear3-b count >= 1, got %d", countAB)
	assert.GreaterOrEqual(t, countBC, 1, "expected callgraph edge linear3-b->linear3-c count >= 1, got %d", countBC)

	statsBBefore := integrationhelpers.GetFunctionCallGraphStats(t, "linear3-b")
	statsCBefore := integrationhelpers.GetFunctionCallGraphStats(t, "linear3-c")

	integrationhelpers.WaitForFunctionsReady(t, []string{"linear3-a", "linear3-b", "linear3-c"}, false, 90*time.Second)

	body = integrationhelpers.InvokeFunctionEventually(t, "linear3-a", 180*time.Second)
	s.assertWorkflowResponse(body)

	cg = integrationhelpers.GetCallGraph(t)
	updatedCount := integrationhelpers.EdgeCount(cg, "", "linear3-a")
	assert.GreaterOrEqual(t, updatedCount, count+1, "expected callgraph edge external->linear3-a count to increase after re-invocation, before=%d after=%d", count, updatedCount)

	updatedCountAB := integrationhelpers.EdgeCount(cg, "linear3-a", "linear3-b")
	updatedCountBC := integrationhelpers.EdgeCount(cg, "linear3-b", "linear3-c")
	assert.GreaterOrEqual(t, updatedCountAB, countAB+1, "expected callgraph edge linear3-a->linear3-b count to increase after re-invocation, before=%d after=%d", countAB, updatedCountAB)
	assert.GreaterOrEqual(t, updatedCountBC, countBC+1, "expected callgraph edge linear3-b->linear3-c count to increase after re-invocation, before=%d after=%d", countBC, updatedCountBC)

	statsBAfter := integrationhelpers.GetFunctionCallGraphStats(t, "linear3-b")
	statsCAfter := integrationhelpers.GetFunctionCallGraphStats(t, "linear3-c")

	assert.GreaterOrEqual(t, statsBAfter.TotalPrewarms, statsBBefore.TotalPrewarms+1, "expected linear3-b prewarm count to increase with prewarming enabled, before=%d after=%d", statsBBefore.TotalPrewarms, statsBAfter.TotalPrewarms)
	assert.GreaterOrEqual(t, statsCAfter.TotalPrewarms, statsCBefore.TotalPrewarms, "expected linear3-c prewarm count to stay the same or increase with prewarming enabled, before=%d after=%d", statsCBefore.TotalPrewarms, statsCAfter.TotalPrewarms)
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
