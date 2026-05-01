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

	// wait 2x the scale-to-zero idle duration to ensure functions have time to scale down
	time.Sleep(25 * time.Second)
	for _, fn := range []string{"linear3-a", "linear3-b", "linear3-c"} {
		status := integrationhelpers.GetFaasdFunction(t, s.baseURL, s.auth, fn)
		require.Equal(t, uint64(1), status.Replicas)
		require.Equal(t, uint64(0), status.AvailableReplicas)
		require.False(t, integrationhelpers.ExistsFaasdContainer(t, fn), "Expected container for function %s to not exist", fn)
	}

	body = integrationhelpers.InvokeFaasdJSON(t, s.baseURL, s.auth, "linear3-a", map[string]any{})
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
	assert.GreaterOrEqual(t, count, 1)
	countAB := integrationhelpers.EdgeCount(cg, "linear3-a", "linear3-b")
	countBC := integrationhelpers.EdgeCount(cg, "linear3-b", "linear3-c")
	assert.GreaterOrEqual(t, countAB, 1)
	assert.GreaterOrEqual(t, countBC, 1)

	statsBBefore := integrationhelpers.GetFaasdFunctionCallGraphStats(t, s.baseURL, s.auth, "linear3-b")
	statsCBefore := integrationhelpers.GetFaasdFunctionCallGraphStats(t, s.baseURL, s.auth, "linear3-c")

	time.Sleep(25 * time.Second)
	body = integrationhelpers.InvokeFaasdJSON(t, s.baseURL, s.auth, "linear3-a", map[string]any{})
	s.assertWorkflowResponse(body)

	cg = integrationhelpers.GetFaasdCallGraph(t, s.baseURL, s.auth)
	updatedCount := integrationhelpers.EdgeCount(cg, "", "linear3-a")
	updatedCountAB := integrationhelpers.EdgeCount(cg, "linear3-a", "linear3-b")
	updatedCountBC := integrationhelpers.EdgeCount(cg, "linear3-b", "linear3-c")
	assert.GreaterOrEqual(t, updatedCount, count+1)
	assert.GreaterOrEqual(t, updatedCountAB, countAB+1)
	assert.GreaterOrEqual(t, updatedCountBC, countBC+1)

	statsBAfter := integrationhelpers.GetFaasdFunctionCallGraphStats(t, s.baseURL, s.auth, "linear3-b")
	statsCAfter := integrationhelpers.GetFaasdFunctionCallGraphStats(t, s.baseURL, s.auth, "linear3-c")

	assert.Equal(t, statsBBefore.TotalPrewarms, statsBAfter.TotalPrewarms)
	assert.Equal(t, statsCBefore.TotalPrewarms, statsCAfter.TotalPrewarms)
	assert.GreaterOrEqual(t, statsBAfter.TotalColdStarts, statsBBefore.TotalColdStarts+1)
	assert.GreaterOrEqual(t, statsCAfter.TotalColdStarts, statsCBefore.TotalColdStarts+1)
}

func (s *PlatformIntegrationSuite) TestAutoscalerAndCallgraphAndPrewarm() {
	t := s.T()
	s.setupScenarioWithEnvs("autoscaler-and-callgraph-and-prewarm.env", map[string]string{"FUNCTION_DELAY_SEC": "5"})

	body := integrationhelpers.InvokeFaasdJSON(t, s.baseURL, s.auth, "linear3-a", map[string]any{})
	s.assertWorkflowResponse(body)

	cg := integrationhelpers.GetFaasdCallGraph(t, s.baseURL, s.auth)
	count := integrationhelpers.EdgeCount(cg, "", "linear3-a")
	assert.GreaterOrEqual(t, count, 1)
	countAB := integrationhelpers.EdgeCount(cg, "linear3-a", "linear3-b")
	countBC := integrationhelpers.EdgeCount(cg, "linear3-b", "linear3-c")
	assert.GreaterOrEqual(t, countAB, 1)
	assert.GreaterOrEqual(t, countBC, 1)

	statsBBefore := integrationhelpers.GetFaasdFunctionCallGraphStats(t, s.baseURL, s.auth, "linear3-b")
	statsCBefore := integrationhelpers.GetFaasdFunctionCallGraphStats(t, s.baseURL, s.auth, "linear3-c")

	time.Sleep(25 * time.Second)
	body = integrationhelpers.InvokeFaasdJSON(t, s.baseURL, s.auth, "linear3-a", map[string]any{})
	s.assertWorkflowResponse(body)

	cg = integrationhelpers.GetFaasdCallGraph(t, s.baseURL, s.auth)
	updatedCount := integrationhelpers.EdgeCount(cg, "", "linear3-a")
	updatedCountAB := integrationhelpers.EdgeCount(cg, "linear3-a", "linear3-b")
	updatedCountBC := integrationhelpers.EdgeCount(cg, "linear3-b", "linear3-c")
	assert.GreaterOrEqual(t, updatedCount, count+1)
	assert.GreaterOrEqual(t, updatedCountAB, countAB+1)
	assert.GreaterOrEqual(t, updatedCountBC, countBC+1)

	statsBAfter := integrationhelpers.GetFaasdFunctionCallGraphStats(t, s.baseURL, s.auth, "linear3-b")
	statsCAfter := integrationhelpers.GetFaasdFunctionCallGraphStats(t, s.baseURL, s.auth, "linear3-c")

	assert.GreaterOrEqual(t, statsBAfter.TotalPrewarms, statsBBefore.TotalPrewarms+1)
	assert.GreaterOrEqual(t, statsCAfter.TotalPrewarms, statsCBefore.TotalPrewarms)
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

	time.Sleep(25 * time.Second)
	for _, fn := range []string{"linear3-a", "linear3-b", "linear3-c", "echo-js"} {
		status := integrationhelpers.GetFaasdFunction(t, s.baseURL, s.auth, fn)
		require.Equal(t, uint64(1), status.Replicas)
		require.Equal(t, uint64(0), status.AvailableReplicas, "expected %s to be scaled down", fn)
	}

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
