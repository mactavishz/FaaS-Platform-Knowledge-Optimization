package faasd

import (
	"os"
	"testing"
	"time"

	integrationhelpers "github.com/mactavishz/FaaS-Platform-Knowledge-Optimization/tests/integration/helpers"
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
	t := s.T()
	integrationhelpers.RebuildFaasd(t, envProfile)
	s.baseURL, s.auth = integrationhelpers.RequireFaasd(t)

	os.Setenv("REGISTRY_PREFIX", "macsalvation/faasd-")
	stackPath := s.repo + "/tests/workflows/faasd/linear3/stack.yaml"
	integrationhelpers.RemoveFaasdWorkflowStack(t, s.baseURL, stackPath)
	t.Cleanup(func() {
		integrationhelpers.RemoveFaasdWorkflowStack(t, s.baseURL, stackPath)
		os.Unsetenv("REGISTRY_PREFIX")
	})

	integrationhelpers.DeployFaasdWorkflowStack(t, s.baseURL, stackPath)
}

func (s *PlatformIntegrationSuite) TestNoAutoscalerNoCallgraph() {
	t := s.T()
	s.setupScenario("no-autoscaler-no-callgraph.env")

	body := integrationhelpers.InvokeFaasdJSONEventually(t, s.baseURL, s.auth, "linear3-a", map[string]any{}, 120*time.Second)
	require.NotEmpty(t, body)

	time.Sleep(15 * time.Second)
	for _, fn := range []string{"linear3-a", "linear3-b", "linear3-c"} {
		require.True(t, integrationhelpers.ExistsFaasdContainer(t, fn), "Expected container for function %s to exist", fn)
		status := integrationhelpers.GetFaasdFunction(t, s.baseURL, s.auth, fn)
		require.Equal(t, uint64(1), status.Replicas)
		require.Equal(t, uint64(1), status.AvailableReplicas)
	}
}

func (s *PlatformIntegrationSuite) TestAutoscalerOnly() {
	t := s.T()
	s.setupScenario("autoscaler-only.env")

	body := integrationhelpers.InvokeFaasdJSONEventually(t, s.baseURL, s.auth, "linear3-a", map[string]any{}, 120*time.Second)
	require.NotEmpty(t, body)

	// wait 2x the scale-to-zero idle duration to ensure functions have time to scale down
	time.Sleep(25 * time.Second)
	for _, fn := range []string{"linear3-a", "linear3-b", "linear3-c"} {
		status := integrationhelpers.GetFaasdFunction(t, s.baseURL, s.auth, fn)
		require.Equal(t, uint64(1), status.Replicas)
		require.Equal(t, uint64(0), status.AvailableReplicas)
		require.False(t, integrationhelpers.ExistsFaasdContainer(t, fn), "Expected container for function %s to not exist", fn)
	}

	body = integrationhelpers.InvokeFaasdJSONEventually(t, s.baseURL, s.auth, "linear3-a", map[string]any{}, 180*time.Second)
	require.NotEmpty(t, body)

	for _, fn := range []string{"linear3-a", "linear3-b", "linear3-c"} {
		status := integrationhelpers.GetFaasdFunction(t, s.baseURL, s.auth, fn)
		require.Equal(t, uint64(1), status.Replicas)
		require.Equal(t, uint64(1), status.AvailableReplicas)
		require.True(t, integrationhelpers.ExistsFaasdContainer(t, fn), "Expected container for function %s to exist", fn)
	}
}
