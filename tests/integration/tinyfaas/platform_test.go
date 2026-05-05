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
	integrationhelpers.WaitForGateway(t, integrationhelpers.DEFAULT_TINYFAAS_GATEWAY_URL, 90*time.Second)
	integrationhelpers.WipeFunctions(t)
	t.Cleanup(func() { integrationhelpers.WipeFunctions(t) })
	integrationhelpers.DeployWorkflowWithEnvs(t, stackPath, deployEnvs)
	integrationhelpers.WaitForFunctionsPresent(t, []string{"linear3-a", "linear3-b", "linear3-c"}, 60*time.Second)
}

func (s *PlatformIntegrationSuite) TestNoAutoscalerNoCallgraph() {
	t := s.T()
	t.Log("[scenario] no autoscaler + no callgraph")
	s.setupScenario("no-autoscaler-no-callgraph.env", integrationhelpers.TINYFAAS_LINEAR3_STACK_FILE_PATH, nil)

	body := integrationhelpers.InvokeFunction(t, "linear3-a", "integration-test", 90*time.Second)
	s.assertWorkflowResponse(body)

	integrationhelpers.WaitForFunctionsRunningState(t, []string{"linear3-a", "linear3-b", "linear3-c"}, true, 15*time.Second)
	// Idle timeout is 12s in profile. With autoscaler disabled, these should stay running.
	time.Sleep(20 * time.Second)
	integrationhelpers.WaitForFunctionsRunningState(t, []string{"linear3-a", "linear3-b", "linear3-c"}, true, 15*time.Second)

	s.assertCallgraphEmpty()
}

func (s *PlatformIntegrationSuite) TestAutoscalerOnly() {
	t := s.T()
	t.Log("[scenario] autoscaler only")
	s.setupScenario("autoscaler-only.env", integrationhelpers.TINYFAAS_LINEAR3_STACK_FILE_PATH, nil)

	body := integrationhelpers.InvokeFunction(t, "linear3-a", "integration-test", 90*time.Second)
	s.assertWorkflowResponse(body)

	// Idle timeout is 12s + autoscaler check interval is 10s.
	integrationhelpers.WaitForFunctionsScaledDown(t, []string{"linear3-a", "linear3-b", "linear3-c"}, 90*time.Second)

	body = integrationhelpers.InvokeFunction(t, "linear3-a", "integration-test", 120*time.Second)
	s.assertWorkflowResponse(body)

	s.assertCallgraphEmpty()
}

func (s *PlatformIntegrationSuite) TestAutoscalerAndCallgraphNoPrewarm() {
	t := s.T()
	t.Log("[scenario] autoscaler + callgraph + prewarm disabled")
	s.setupScenario(
		"autoscaler-and-callgraph-no-prewarm.env",
		integrationhelpers.TINYFAAS_LINEAR3_STACK_FILE_PATH,
		map[string]string{"FUNCTION_DELAY_SEC": "5"},
	)

	body := integrationhelpers.InvokeFunction(t, "linear3-a", "integration-test", 90*time.Second)
	s.assertWorkflowResponse(body)

	cg := integrationhelpers.GetCallGraph(t)

	count := integrationhelpers.EdgeCount(cg, "", "linear3-a")
	countAB := integrationhelpers.EdgeCount(cg, "linear3-a", "linear3-b")
	countBC := integrationhelpers.EdgeCount(cg, "linear3-b", "linear3-c")
	statsA := integrationhelpers.GetFunctionCallGraphStats(t, "linear3-a")
	statsB := integrationhelpers.GetFunctionCallGraphStats(t, "linear3-b")
	statsC := integrationhelpers.GetFunctionCallGraphStats(t, "linear3-c")

	// check callgraph edges are present with expected counts
	assert.Equal(t, count, 1, "expected callgraph edge external->linear3-a count = 1, got %d", count)
	assert.Equal(t, countAB, 1, "expected callgraph edge linear3-a->linear3-b count = 1, got %d", countAB)
	assert.Equal(t, countBC, 1, "expected callgraph edge linear3-b->linear3-c count = 1, got %d", countBC)

	// check names are correct in stats response
	assert.Equal(t, statsA.Name, "linear3-a", "expected linear3-a stats name to be linear3-a, got %s", statsA.Name)
	assert.Equal(t, statsB.Name, "linear3-b", "expected linear3-b stats name to be linear3-b, got %s", statsB.Name)
	assert.Equal(t, statsC.Name, "linear3-c", "expected linear3-c stats name to be linear3-c, got %s", statsC.Name)

	// check total calls are 1 for each function
	assert.Equal(t, statsA.TotalCalls, 1, "expected linear3-a total calls to be 1, got %d", statsA.TotalCalls)
	assert.Equal(t, statsB.TotalCalls, 1, "expected linear3-b total calls to be 1, got %d", statsB.TotalCalls)
	assert.Equal(t, statsC.TotalCalls, 1, "expected linear3-c total calls to be 1, got %d", statsC.TotalCalls)

	// check cold starts are 1 for each function since they should all scale from 0
	assert.Equal(t, statsA.TotalColdStarts, 1, "expected linear3-a total cold starts to be 1, got %d", statsA.TotalColdStarts)
	assert.Equal(t, statsB.TotalColdStarts, 1, "expected linear3-b total cold starts to be 1, got %d", statsB.TotalColdStarts)
	assert.Equal(t, statsC.TotalColdStarts, 1, "expected linear3-c total cold starts to be 1, got %d", statsC.TotalColdStarts)

	// check prewarm counts are 0 with prewarm disabled
	assert.Equal(t, statsA.TotalPrewarms, 0, "expected linear3-a prewarm count to be 0, got %d", statsA.TotalPrewarms)
	assert.Equal(t, statsB.TotalPrewarms, 0, "expected linear3-b prewarm count to be 0, got %d", statsB.TotalPrewarms)
	assert.Equal(t, statsC.TotalPrewarms, 0, "expected linear3-c prewarm count to be 0, got %d", statsC.TotalPrewarms)
}

func (s *PlatformIntegrationSuite) TestAutoscalerAndCallgraphAndPrewarm() {
	t := s.T()
	t.Log("[scenario] autoscaler + callgraph + prewarm enabled")
	s.setupScenario(
		"autoscaler-and-callgraph-and-prewarm.env",
		integrationhelpers.TINYFAAS_LINEAR3_STACK_FILE_PATH,
		map[string]string{"FUNCTION_DELAY_SEC": "5"},
	)

	body := integrationhelpers.InvokeFunction(t, "linear3-a", "integration-test", 90*time.Second)
	s.assertWorkflowResponse(body)

	cg := integrationhelpers.GetCallGraph(t)
	count := integrationhelpers.EdgeCount(cg, "", "linear3-a")
	countAB := integrationhelpers.EdgeCount(cg, "linear3-a", "linear3-b")
	countBC := integrationhelpers.EdgeCount(cg, "linear3-b", "linear3-c")
	statsBBefore := integrationhelpers.GetFunctionCallGraphStats(t, "linear3-b")
	statsCBefore := integrationhelpers.GetFunctionCallGraphStats(t, "linear3-c")

	assert.Equal(t, count, 1, "expected callgraph edge external->linear3-a count = 1, got %d", count)
	assert.Equal(t, countAB, 1, "expected callgraph edge linear3-a->linear3-b count = 1, got %d", countAB)
	assert.Equal(t, countBC, 1, "expected callgraph edge linear3-b->linear3-c count = 1, got %d", countBC)

	integrationhelpers.WaitForFunctionsScaledDown(t, []string{"linear3-a", "linear3-b", "linear3-c"}, 90*time.Second)

	body = integrationhelpers.InvokeFunction(t, "linear3-a", "integration-test", 180*time.Second)
	s.assertWorkflowResponse(body)

	cg = integrationhelpers.GetCallGraph(t)
	updatedCount := integrationhelpers.EdgeCount(cg, "", "linear3-a")
	updatedCountAB := integrationhelpers.EdgeCount(cg, "linear3-a", "linear3-b")
	updatedCountBC := integrationhelpers.EdgeCount(cg, "linear3-b", "linear3-c")
	statsBAfter := integrationhelpers.GetFunctionCallGraphStats(t, "linear3-b")
	statsCAfter := integrationhelpers.GetFunctionCallGraphStats(t, "linear3-c")

	assert.Equal(t, updatedCount, count+1, "expected callgraph edge external->linear3-a count to increase after re-invocation, before=%d after=%d", count, updatedCount)
	assert.Equal(t, updatedCountAB, countAB+1, "expected callgraph edge linear3-a->linear3-b count to increase after re-invocation, before=%d after=%d", countAB, updatedCountAB)
	assert.Equal(t, updatedCountBC, countBC+1, "expected callgraph edge linear3-b->linear3-c count to increase after re-invocation, before=%d after=%d", countBC, updatedCountBC)

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
