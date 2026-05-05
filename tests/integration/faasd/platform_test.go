package faasd

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
	baseURL string
	auth    integrationhelpers.FaasdGatewayAuth
	repo    string
}

func TestPlatformIntegrationSuite(t *testing.T) {
	suite.Run(t, new(PlatformIntegrationSuite))
}

func (s *PlatformIntegrationSuite) SetupSuite() {
	t := s.T()
	s.repo = integrationhelpers.RepoRoot(t)
	s.baseURL, s.auth = integrationhelpers.RequireFaasd(t)
}

func (s *PlatformIntegrationSuite) setupScenario(envProfile string) {
	s.setupScenarioWithEnvs(envProfile, nil)
}

func (s *PlatformIntegrationSuite) setupScenarioWithEnvs(envProfile string, deployEnvs map[string]string) {
	t := s.T()
	integrationhelpers.RebuildFaasd(t, envProfile)
	s.baseURL, s.auth = integrationhelpers.RequireFaasd(t)

	stackPath := s.repo + "/tests/workflows/faasd/linear3/stack.yaml"
	integrationhelpers.RemoveFaasdWorkflowStack(t, s.baseURL, stackPath)
	t.Cleanup(func() {
		integrationhelpers.RemoveFaasdWorkflowStack(t, s.baseURL, stackPath)
	})

	integrationhelpers.PrepareFaasdWorkflowStack(t, stackPath)
	integrationhelpers.DeployFaasdWorkflowStackWithEnvs(t, s.baseURL, stackPath, deployEnvs)
}

func (s *PlatformIntegrationSuite) setupAsyncCallbackScenario(envProfile string) {
	t := s.T()
	s.setupScenario(envProfile)

	echoStackPath := s.repo + "/faasd/test/fns/echo-js/stack.yaml"
	integrationhelpers.RemoveFaasdWorkflowStack(t, s.baseURL, echoStackPath)
	t.Cleanup(func() {
		integrationhelpers.RemoveFaasdWorkflowStack(t, s.baseURL, echoStackPath)
	})
	integrationhelpers.PrepareFaasdWorkflowStack(t, echoStackPath)
	integrationhelpers.DeployFaasdWorkflowStack(t, s.baseURL, echoStackPath)
}

func (s *PlatformIntegrationSuite) TestNoAutoscalerNoCallgraph() {
	t := s.T()
	s.setupScenario("no-autoscaler-no-callgraph.env")

	body := integrationhelpers.InvokeFaasdJSON(t, s.baseURL, s.auth, "linear3-a", map[string]any{})
	require.NotEmpty(t, body)
	s.assertWorkflowResponse(body)

	time.Sleep(15 * time.Second)
	for _, fn := range []string{"linear3-a", "linear3-b", "linear3-c"} {
		require.True(t, integrationhelpers.ExistsFaasdContainer(t, fn), "Expected container for function %s to exist", fn)
		status := integrationhelpers.GetFaasdFunction(t, s.baseURL, s.auth, fn)
		require.Equal(t, uint64(1), status.Replicas)
		require.Equal(t, uint64(1), status.AvailableReplicas)
	}

	s.assertCallgraphEmpty()
}

func (s *PlatformIntegrationSuite) TestAutoscalerOnly() {
	t := s.T()
	s.setupScenario("autoscaler-only.env")

	body := integrationhelpers.InvokeFaasdJSON(t, s.baseURL, s.auth, "linear3-a", map[string]any{})
	require.NotEmpty(t, body)
	s.assertWorkflowResponse(body)

	s.waitForFaasdFunctionsScaledDown([]string{"linear3-a", "linear3-b", "linear3-c"}, 90*time.Second)

	body = integrationhelpers.InvokeFaasdJSONWithTimeout(t, s.baseURL, s.auth, "linear3-a", map[string]any{}, 120*time.Second)
	require.NotEmpty(t, body)
	s.assertWorkflowResponse(body)

	for _, fn := range []string{"linear3-a", "linear3-b", "linear3-c"} {
		status := integrationhelpers.GetFaasdFunction(t, s.baseURL, s.auth, fn)
		require.Equal(t, uint64(1), status.Replicas)
		require.Equal(t, uint64(1), status.AvailableReplicas)
		require.True(t, integrationhelpers.ExistsFaasdContainer(t, fn), "Expected container for function %s to exist", fn)
	}

	s.assertCallgraphEmpty()
}

func (s *PlatformIntegrationSuite) TestAutoscalerAndCallgraphNoPrewarm() {
	t := s.T()
	s.setupScenarioWithEnvs("autoscaler-and-callgraph-no-prewarm.env", map[string]string{"FUNCTION_DELAY_SEC": "5"})

	body := integrationhelpers.InvokeFaasdJSON(t, s.baseURL, s.auth, "linear3-a", map[string]any{})
	s.assertWorkflowResponse(body)

	cg := integrationhelpers.GetFaasdCallGraph(t, s.baseURL, s.auth)

	count := integrationhelpers.EdgeCount(cg, "", "linear3-a")
	countAB := integrationhelpers.EdgeCount(cg, "linear3-a", "linear3-b")
	countBC := integrationhelpers.EdgeCount(cg, "linear3-b", "linear3-c")
	statsA := integrationhelpers.GetFaasdFunctionCallGraphStats(t, s.baseURL, s.auth, "linear3-a")
	statsB := integrationhelpers.GetFaasdFunctionCallGraphStats(t, s.baseURL, s.auth, "linear3-b")
	statsC := integrationhelpers.GetFaasdFunctionCallGraphStats(t, s.baseURL, s.auth, "linear3-c")

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
	s.setupScenarioWithEnvs("autoscaler-and-callgraph-and-prewarm.env", map[string]string{"FUNCTION_DELAY_SEC": "5"})

	body := integrationhelpers.InvokeFaasdJSON(t, s.baseURL, s.auth, "linear3-a", map[string]any{})
	s.assertWorkflowResponse(body)

	cg := integrationhelpers.GetFaasdCallGraph(t, s.baseURL, s.auth)

	count := integrationhelpers.EdgeCount(cg, "", "linear3-a")
	countAB := integrationhelpers.EdgeCount(cg, "linear3-a", "linear3-b")
	countBC := integrationhelpers.EdgeCount(cg, "linear3-b", "linear3-c")
	statsBBefore := integrationhelpers.GetFaasdFunctionCallGraphStats(t, s.baseURL, s.auth, "linear3-b")
	statsCBefore := integrationhelpers.GetFaasdFunctionCallGraphStats(t, s.baseURL, s.auth, "linear3-c")

	assert.Equal(t, count, 1, "expected callgraph edge external->linear3-a count = 1, got %d", count)
	assert.Equal(t, countAB, 1, "expected callgraph edge linear3-a->linear3-b count = 1, got %d", countAB)
	assert.Equal(t, countBC, 1, "expected callgraph edge linear3-b->linear3-c count = 1, got %d", countBC)

	s.waitForFaasdFunctionsScaledDown([]string{"linear3-a", "linear3-b", "linear3-c"}, 90*time.Second)
	body = integrationhelpers.InvokeFaasdJSONWithTimeout(t, s.baseURL, s.auth, "linear3-a", map[string]any{}, 180*time.Second)
	s.assertWorkflowResponse(body)

	cg = integrationhelpers.GetFaasdCallGraph(t, s.baseURL, s.auth)
	updatedCount := integrationhelpers.EdgeCount(cg, "", "linear3-a")
	updatedCountAB := integrationhelpers.EdgeCount(cg, "linear3-a", "linear3-b")
	updatedCountBC := integrationhelpers.EdgeCount(cg, "linear3-b", "linear3-c")
	statsBAfter := integrationhelpers.GetFaasdFunctionCallGraphStats(t, s.baseURL, s.auth, "linear3-b")
	statsCAfter := integrationhelpers.GetFaasdFunctionCallGraphStats(t, s.baseURL, s.auth, "linear3-c")

	assert.Equal(t, updatedCount, count+1, "expected callgraph edge external->linear3-a count to be incremented by 1 after second invocation, got %d", updatedCount)
	assert.Equal(t, updatedCountAB, countAB+1, "expected callgraph edge linear3-a->linear3-b count to be incremented by 1 after second invocation, got %d", updatedCountAB)
	assert.Equal(t, updatedCountBC, countBC+1, "expected callgraph edge linear3-b->linear3-c count to be incremented by 1 after second invocation, got %d", updatedCountBC)

	assert.GreaterOrEqual(t, statsBAfter.TotalPrewarms, statsBBefore.TotalPrewarms+1, "expected linear3-b total prewarms to be incremented by at least 1 after second invocation, got %d", statsBAfter.TotalPrewarms)
	assert.GreaterOrEqual(t, statsCAfter.TotalPrewarms, statsCBefore.TotalPrewarms, "expected linear3-c total prewarms to be incremented by at least 1 after second invocation, got %d", statsCAfter.TotalPrewarms)
}

func (s *PlatformIntegrationSuite) assertCallgraphEmpty() {
	t := s.T()
	t.Helper()

	cg := integrationhelpers.GetFaasdCallGraph(t, s.baseURL, s.auth)
	assert.Empty(t, cg.Edges)
	assert.Empty(t, cg.Functions)
}

func (s *PlatformIntegrationSuite) assertWorkflowResponse(body []byte) {
	t := s.T()
	t.Helper()

	var payload map[string]any
	err := json.Unmarshal(body, &payload)
	require.NoError(t, err, "expected JSON response from workflow, got: %s", string(body))
	assert.NotNil(t, payload["msg"], "expected workflow response to contain msg, got: %s", string(body))
}

func (s *PlatformIntegrationSuite) TestAutoscalerAsyncWithCallback() {
	t := s.T()
	s.setupAsyncCallbackScenario("autoscaler-only.env")

	// Warm up once so that autoscaler has registered activity and can scale down all fns.
	body := integrationhelpers.InvokeFaasdJSON(t, s.baseURL, s.auth, "linear3-a", map[string]any{})
	require.NotEmpty(t, body)

	s.waitForFaasdFunctionsScaledDown([]string{"linear3-a", "linear3-b", "linear3-c", "echo-js"}, 120*time.Second)

	callbackURL := "http://gateway:8080/function/echo-js"
	statusCode, asyncBody := integrationhelpers.InvokeFaasdAsyncJSON(t, s.baseURL, s.auth, "linear3-a", map[string]any{}, callbackURL)
	assert.Equal(t, 202, statusCode, "expected async enqueue accepted, body=%s", string(asyncBody))

	integrationhelpers.WaitForFaasdServiceLogMatch(t, "queue-worker", "Posted result for linear3-a to callback-url", 120*time.Second)
	integrationhelpers.Eventually(t, 120*time.Second, 1*time.Second, func() bool {
		callbackStatus := integrationhelpers.GetFaasdFunction(t, s.baseURL, s.auth, "echo-js")
		return callbackStatus.AvailableReplicas == 1
	})
	integrationhelpers.WaitForFaasdFunctionJournalLogMatch(t, "echo-js", "Function linear3-a is finished", 120*time.Second)
	integrationhelpers.WaitForFaasdFunctionJournalLogMatch(t, "echo-js", "Function linear3-b is finished", 120*time.Second)
	integrationhelpers.WaitForFaasdFunctionJournalLogMatch(t, "echo-js", "Function linear3-c is finished", 120*time.Second)

	callbackStatus := integrationhelpers.GetFaasdFunction(t, s.baseURL, s.auth, "echo-js")
	assert.Equal(t, uint64(1), callbackStatus.Replicas)
	assert.Equal(t, uint64(1), callbackStatus.AvailableReplicas)
}

func (s *PlatformIntegrationSuite) waitForFaasdFunctionsScaledDown(functions []string, timeout time.Duration) {
	t := s.T()
	t.Helper()

	integrationhelpers.Eventually(t, timeout, 1*time.Second, func() bool {
		for _, fn := range functions {
			status := integrationhelpers.GetFaasdFunction(t, s.baseURL, s.auth, fn)
			if status.Replicas != 1 || status.AvailableReplicas != 0 {
				return false
			}
			if integrationhelpers.ExistsFaasdContainer(t, fn) {
				return false
			}
		}
		return true
	})
}
