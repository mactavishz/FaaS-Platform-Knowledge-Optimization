package faasd

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	integrationhelpers "github.com/mactavishz/FaaS-Platform-Knowledge-Optimization/tests/integration/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

const (
	faasdLinear3StackRelPath  = "tests/workflows/faasd/linear3/stack.yaml"
	faasdTreeStackRelPath     = "tests/workflows/faasd/tree/stack.yaml"
	faasdIoTStackRelPath      = "tests/workflows/faasd/IoT/stack.yaml"
	faasdIoTEnvRelPath        = "tests/workflows/faasd/IoT/.env.yaml"
	faasdWebshopStackRelPath  = "tests/workflows/faasd/webshop/stack.yaml"
	faasdWebshopEnvRelPath    = "tests/workflows/faasd/webshop/.env.yaml"
	callgraphWaitTimeout      = 120 * time.Second
	callgraphPollInterval     = 1 * time.Second
	webshopCheckoutTimeout    = 180 * time.Second
	workflowInvocationTimeout = 120 * time.Second
)

type callGraphEdgeExpectation struct {
	Caller string
	Callee string
	Count  int
}

type functionStatsExpectation struct {
	Name       string
	ExactCalls int
	MinCalls   int
}

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

func (s *PlatformIntegrationSuite) setupScenario(envProfile string, stackRelPath string, deployEnvs map[string]string) []string {
	t := s.T()
	integrationhelpers.RebuildFaasd(t, envProfile)
	s.baseURL, s.auth = integrationhelpers.RequireFaasd(t)

	stackPath := filepath.Join(s.repo, stackRelPath)
	functionNames := integrationhelpers.FaasdStackFunctionNames(t, stackPath)
	integrationhelpers.RemoveFaasdWorkflowStack(t, s.baseURL, stackPath)
	t.Cleanup(func() {
		integrationhelpers.RemoveFaasdWorkflowStack(t, s.baseURL, stackPath)
	})

	integrationhelpers.PrepareFaasdWorkflowStack(t, stackPath)
	integrationhelpers.DeployFaasdWorkflowStackWithEnvs(t, s.baseURL, stackPath, deployEnvs)
	integrationhelpers.WaitForFaasdFunctionsPresent(t, s.baseURL, s.auth, functionNames, workflowInvocationTimeout)
	return functionNames
}

func (s *PlatformIntegrationSuite) setupAsyncCallbackScenario(envProfile string) {
	t := s.T()
	s.setupScenario(envProfile, faasdLinear3StackRelPath, nil)

	echoStackPath := filepath.Join(s.repo, "faasd/test/fns/echo-js/stack.yaml")
	echoFunctionNames := integrationhelpers.FaasdStackFunctionNames(t, echoStackPath)
	integrationhelpers.RemoveFaasdWorkflowStack(t, s.baseURL, echoStackPath)
	t.Cleanup(func() {
		integrationhelpers.RemoveFaasdWorkflowStack(t, s.baseURL, echoStackPath)
	})
	integrationhelpers.PrepareFaasdWorkflowStack(t, echoStackPath)
	integrationhelpers.DeployFaasdWorkflowStack(t, s.baseURL, echoStackPath)
	integrationhelpers.WaitForFaasdFunctionsPresent(t, s.baseURL, s.auth, echoFunctionNames, workflowInvocationTimeout)
}

func (s *PlatformIntegrationSuite) TestNoAutoscalerNoCallgraph() {
	t := s.T()
	s.setupScenario("no-autoscaler-no-callgraph.env", faasdLinear3StackRelPath, nil)

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
	s.setupScenario("autoscaler-only.env", faasdLinear3StackRelPath, nil)

	body := integrationhelpers.InvokeFaasdJSON(t, s.baseURL, s.auth, "linear3-a", map[string]any{})
	require.NotEmpty(t, body)
	s.assertWorkflowResponse(body)

	s.waitForFaasdFunctionsScaledDown([]string{"linear3-a", "linear3-b", "linear3-c"}, 90*time.Second)

	body = integrationhelpers.InvokeFaasdJSONWithTimeout(t, s.baseURL, s.auth, "linear3-a", map[string]any{}, workflowInvocationTimeout)
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
	s.setupScenario(
		"autoscaler-and-callgraph-no-prewarm.env",
		faasdLinear3StackRelPath,
		map[string]string{"FUNCTION_DELAY_SEC": "5"},
	)

	body := integrationhelpers.InvokeFaasdJSONWithTimeout(t, s.baseURL, s.auth, "linear3-a", map[string]any{}, workflowInvocationTimeout)
	s.assertWorkflowResponse(body)

	s.waitForFaasdCallgraphEdges([]callGraphEdgeExpectation{
		{Caller: "", Callee: "linear3-a", Count: 1},
		{Caller: "linear3-a", Callee: "linear3-b", Count: 1},
		{Caller: "linear3-b", Callee: "linear3-c", Count: 1},
	}, callgraphWaitTimeout)
	s.assertFaasdFunctionStats([]functionStatsExpectation{
		{Name: "linear3-a", ExactCalls: 1},
		{Name: "linear3-b", ExactCalls: 1},
		{Name: "linear3-c", ExactCalls: 1},
	})
}

func (s *PlatformIntegrationSuite) TestAutoscalerAndCallgraphNoPrewarmTree() {
	t := s.T()
	s.setupScenario("autoscaler-and-callgraph-no-prewarm.env", faasdTreeStackRelPath, nil)

	traceID := "tree-platform"
	body := integrationhelpers.InvokeFaasdJSONWithTimeout(t, s.baseURL, s.auth, "tree-a", map[string]any{"traceId": traceID}, workflowInvocationTimeout)
	s.assertTreeResponse(body, traceID)

	s.waitForFaasdCallgraphEdges([]callGraphEdgeExpectation{
		{Caller: "", Callee: "tree-a", Count: 1},
		{Caller: "tree-a", Callee: "tree-b", Count: 1},
		{Caller: "tree-a", Callee: "tree-c", Count: 1},
		{Caller: "tree-b", Callee: "tree-d", Count: 1},
		{Caller: "tree-b", Callee: "tree-e", Count: 1},
		{Caller: "tree-c", Callee: "tree-f", Count: 1},
		{Caller: "tree-c", Callee: "tree-g", Count: 1},
	}, callgraphWaitTimeout)
	s.assertFaasdFunctionStats([]functionStatsExpectation{
		{Name: "tree-a", ExactCalls: 1},
		{Name: "tree-b", ExactCalls: 1},
		{Name: "tree-c", ExactCalls: 1},
		{Name: "tree-d", ExactCalls: 1},
		{Name: "tree-e", ExactCalls: 1},
		{Name: "tree-f", ExactCalls: 1},
		{Name: "tree-g", ExactCalls: 1},
	})
}

func (s *PlatformIntegrationSuite) TestAutoscalerAndCallgraphNoPrewarmIoT() {
	t := s.T()
	integrationhelpers.RequireWorkflowEnvFile(t, faasdIoTEnvRelPath)
	s.setupScenario("autoscaler-and-callgraph-no-prewarm.env", faasdIoTStackRelPath, nil)

	body := integrationhelpers.InvokeFaasdJSONWithTimeout(t, s.baseURL, s.auth, "iot-i", map[string]any{}, workflowInvocationTimeout)
	s.assertIoTResponse(body)

	s.waitForFaasdCallgraphEdges([]callGraphEdgeExpectation{
		{Caller: "", Callee: "iot-i", Count: 1},
		{Caller: "iot-i", Callee: "iot-cw", Count: 1},
		{Caller: "iot-i", Callee: "iot-se", Count: 1},
		{Caller: "iot-i", Callee: "iot-ct", Count: 1},
		{Caller: "iot-i", Callee: "iot-cs", Count: 1},
		{Caller: "iot-i", Callee: "iot-ca", Count: 1},
		{Caller: "iot-ct", Callee: "iot-as", Count: 1},
		{Caller: "iot-cs", Callee: "iot-csl", Count: 1},
		{Caller: "iot-cs", Callee: "iot-csa", Count: 1},
		{Caller: "iot-ca", Callee: "iot-dj", Count: 1},
		{Caller: "iot-ca", Callee: "iot-as", Count: 1},
	}, callgraphWaitTimeout)
	s.assertFaasdFunctionStats([]functionStatsExpectation{
		{Name: "iot-i", ExactCalls: 1},
		{Name: "iot-cw", ExactCalls: 1},
		{Name: "iot-se", ExactCalls: 1},
		{Name: "iot-cs", ExactCalls: 1},
		{Name: "iot-ca", ExactCalls: 1},
		{Name: "iot-csl", ExactCalls: 1},
		{Name: "iot-csa", ExactCalls: 1},
		{Name: "iot-dj", ExactCalls: 1},
	})
}

func (s *PlatformIntegrationSuite) TestAutoscalerAndCallgraphNoPrewarmWebshop() {
	t := s.T()
	integrationhelpers.RequireWorkflowEnvFile(t, faasdWebshopEnvRelPath)
	s.setupScenario("autoscaler-and-callgraph-no-prewarm.env", faasdWebshopStackRelPath, nil)

	userID := uniqueTestUserID("platform-webshop")
	baseline := integrationhelpers.GetFaasdCallGraph(t, s.baseURL, s.auth)
	getBody := integrationhelpers.InvokeFaasdJSONWithTimeout(t, s.baseURL, s.auth, "webshop-frontend", map[string]any{
		"operation": "get",
		"userId":    userID,
		"currency":  "USD",
	}, workflowInvocationTimeout)
	s.assertWebshopGetResponse(getBody)
	s.waitForFaasdCallgraphEdgeDeltas(baseline, []callGraphEdgeExpectation{
		{Caller: "", Callee: "webshop-frontend", Count: 1},
		{Caller: "webshop-frontend", Callee: "webshop-supportedcurrencies", Count: 1},
		{Caller: "webshop-frontend", Callee: "webshop-listproducts", Count: 1},
		{Caller: "webshop-frontend", Callee: "webshop-currency", Count: 11},
		{Caller: "webshop-frontend", Callee: "webshop-getads", Count: 1},
		{Caller: "webshop-frontend", Callee: "webshop-getcart", Count: 1},
		{Caller: "webshop-getcart", Callee: "webshop-cartstorage", Count: 1},
		{Caller: "webshop-frontend", Callee: "webshop-listrecommendations", Count: 1},
		{Caller: "webshop-listrecommendations", Callee: "webshop-listproducts", Count: 1},
	}, callgraphWaitTimeout)

	baseline = integrationhelpers.GetFaasdCallGraph(t, s.baseURL, s.auth)
	cartBody := integrationhelpers.InvokeFaasdJSONWithTimeout(t, s.baseURL, s.auth, "webshop-frontend", map[string]any{
		"operation": "cart",
		"userId":    userID,
	}, workflowInvocationTimeout)
	s.assertWebshopCartResponse(cartBody, 0, 0)
	s.waitForFaasdCallgraphEdgeDeltas(baseline, []callGraphEdgeExpectation{
		{Caller: "", Callee: "webshop-frontend", Count: 1},
		{Caller: "webshop-frontend", Callee: "webshop-getcart", Count: 1},
		{Caller: "webshop-getcart", Callee: "webshop-cartstorage", Count: 1},
		{Caller: "webshop-frontend", Callee: "webshop-shipmentquote", Count: 1},
	}, callgraphWaitTimeout)

	baseline = integrationhelpers.GetFaasdCallGraph(t, s.baseURL, s.auth)
	addCartBody := integrationhelpers.InvokeFaasdJSONWithTimeout(t, s.baseURL, s.auth, "webshop-frontend", map[string]any{
		"operation": "addcart",
		"userId":    userID,
		"productId": "3",
		"quantity":  1,
	}, workflowInvocationTimeout)
	s.assertWebshopAddCartResponse(addCartBody, userID)
	s.waitForFaasdCallgraphEdgeDeltas(baseline, []callGraphEdgeExpectation{
		{Caller: "", Callee: "webshop-frontend", Count: 1},
		{Caller: "webshop-frontend", Callee: "webshop-addcartitem", Count: 1},
		{Caller: "webshop-addcartitem", Callee: "webshop-cartstorage", Count: 1},
		{Caller: "webshop-frontend", Callee: "webshop-getcart", Count: 1},
		{Caller: "webshop-getcart", Callee: "webshop-cartstorage", Count: 1},
	}, callgraphWaitTimeout)

	baseline = integrationhelpers.GetFaasdCallGraph(t, s.baseURL, s.auth)
	checkoutBody := integrationhelpers.InvokeFaasdJSONWithTimeout(t, s.baseURL, s.auth, "webshop-frontend", map[string]any{
		"operation": "checkout",
		"userId":    userID,
		"currency":  "EUR",
		"address": map[string]any{
			"street": "123 Main St",
		},
	}, webshopCheckoutTimeout)
	s.assertWebshopUserIDResponse(checkoutBody, userID)

	s.waitForFaasdCallgraphEdgeDeltas(baseline, []callGraphEdgeExpectation{
		{Caller: "", Callee: "webshop-frontend", Count: 1},
		{Caller: "webshop-frontend", Callee: "webshop-checkout", Count: 1},
		{Caller: "webshop-checkout", Callee: "webshop-getcart", Count: 1},
		{Caller: "webshop-getcart", Callee: "webshop-cartstorage", Count: 1},
		{Caller: "webshop-checkout", Callee: "webshop-listproducts", Count: 1},
		{Caller: "webshop-checkout", Callee: "webshop-currency", Count: 2},
		{Caller: "webshop-checkout", Callee: "webshop-shipmentquote", Count: 1},
		{Caller: "webshop-checkout", Callee: "webshop-shiporder", Count: 1},
		{Caller: "webshop-checkout", Callee: "webshop-email", Count: 1},
		{Caller: "webshop-checkout", Callee: "webshop-emptycart", Count: 1},
		{Caller: "webshop-emptycart", Callee: "webshop-cartstorage", Count: 1},
	}, callgraphWaitTimeout)

	baseline = integrationhelpers.GetFaasdCallGraph(t, s.baseURL, s.auth)
	emptyCartBody := integrationhelpers.InvokeFaasdJSONWithTimeout(t, s.baseURL, s.auth, "webshop-frontend", map[string]any{
		"operation": "emptycart",
		"userId":    userID,
	}, workflowInvocationTimeout)
	s.assertWebshopUserIDResponse(emptyCartBody, userID)
	s.waitForFaasdCallgraphEdgeDeltas(baseline, []callGraphEdgeExpectation{
		{Caller: "", Callee: "webshop-frontend", Count: 1},
		{Caller: "webshop-frontend", Callee: "webshop-emptycart", Count: 1},
		{Caller: "webshop-emptycart", Callee: "webshop-cartstorage", Count: 1},
	}, callgraphWaitTimeout)

	s.assertFaasdFunctionStats([]functionStatsExpectation{
		{Name: "webshop-frontend", ExactCalls: 5},
		{Name: "webshop-supportedcurrencies", ExactCalls: 1},
		{Name: "webshop-listproducts", ExactCalls: 3},
		{Name: "webshop-currency", ExactCalls: 13},
		{Name: "webshop-getads", ExactCalls: 1},
		{Name: "webshop-listrecommendations", ExactCalls: 1},
		{Name: "webshop-addcartitem", ExactCalls: 1},
		{Name: "webshop-checkout", ExactCalls: 1},
		{Name: "webshop-getcart", ExactCalls: 4},
		{Name: "webshop-cartstorage", MinCalls: 5},
		{Name: "webshop-shipmentquote", ExactCalls: 2},
		{Name: "webshop-shiporder", ExactCalls: 1},
		{Name: "webshop-email", ExactCalls: 1},
		{Name: "webshop-emptycart", ExactCalls: 2},
	})
}

func (s *PlatformIntegrationSuite) TestAutoscalerAndCallgraphAndPrewarm() {
	t := s.T()
	s.setupScenario(
		"autoscaler-and-callgraph-and-prewarm.env",
		faasdLinear3StackRelPath,
		map[string]string{"FUNCTION_DELAY_SEC": "5"},
	)

	body := integrationhelpers.InvokeFaasdJSONWithTimeout(t, s.baseURL, s.auth, "linear3-a", map[string]any{}, workflowInvocationTimeout)
	s.assertWorkflowResponse(body)

	s.waitForFaasdCallgraphEdges([]callGraphEdgeExpectation{
		{Caller: "", Callee: "linear3-a", Count: 1},
		{Caller: "linear3-a", Callee: "linear3-b", Count: 1},
		{Caller: "linear3-b", Callee: "linear3-c", Count: 1},
	}, callgraphWaitTimeout)
	statsBBefore := integrationhelpers.GetFaasdFunctionCallGraphStats(t, s.baseURL, s.auth, "linear3-b")
	statsCBefore := integrationhelpers.GetFaasdFunctionCallGraphStats(t, s.baseURL, s.auth, "linear3-c")

	s.waitForFaasdFunctionsScaledDown([]string{"linear3-a", "linear3-b", "linear3-c"}, 90*time.Second)
	body = integrationhelpers.InvokeFaasdJSONWithTimeout(t, s.baseURL, s.auth, "linear3-a", map[string]any{}, webshopCheckoutTimeout)
	s.assertWorkflowResponse(body)

	s.waitForFaasdCallgraphEdges([]callGraphEdgeExpectation{
		{Caller: "", Callee: "linear3-a", Count: 2},
		{Caller: "linear3-a", Callee: "linear3-b", Count: 2},
		{Caller: "linear3-b", Callee: "linear3-c", Count: 2},
	}, callgraphWaitTimeout)
	statsBAfter := integrationhelpers.GetFaasdFunctionCallGraphStats(t, s.baseURL, s.auth, "linear3-b")
	statsCAfter := integrationhelpers.GetFaasdFunctionCallGraphStats(t, s.baseURL, s.auth, "linear3-c")

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

func (s *PlatformIntegrationSuite) assertTreeResponse(body []byte, traceID string) {
	t := s.T()
	t.Helper()

	var payload struct {
		From    string           `json:"from"`
		TraceID string           `json:"traceId"`
		Results []map[string]any `json:"results"`
		Checked []map[string]any `json:"checked"`
	}
	err := json.Unmarshal(body, &payload)
	require.NoError(t, err, "expected JSON response from tree workflow, got: %s", string(body))
	assert.Equal(t, "A", payload.From)
	assert.Equal(t, traceID, payload.TraceID)
	assert.Len(t, payload.Results, 1)
	assert.Len(t, payload.Checked, 1)
}

func (s *PlatformIntegrationSuite) assertIoTResponse(body []byte) {
	t := s.T()
	t.Helper()

	var payload struct {
		Results []map[string]any `json:"results"`
	}
	err := json.Unmarshal(body, &payload)
	require.NoError(t, err, "expected JSON response from IoT workflow, got: %s", string(body))
	assert.Len(t, payload.Results, 3)
}

func (s *PlatformIntegrationSuite) assertWebshopAddCartResponse(body []byte, userID string) {
	t := s.T()
	t.Helper()

	var payload struct {
		Cart []struct {
			UserID   string `json:"userId"`
			ItemID   string `json:"itemId"`
			Quantity int    `json:"quantity"`
		} `json:"cart"`
	}
	err := json.Unmarshal(body, &payload)
	require.NoError(t, err, "expected JSON response from webshop addcart, got: %s", string(body))
	require.Len(t, payload.Cart, 1)
	assert.Equal(t, userID, payload.Cart[0].UserID)
	assert.Equal(t, "3", payload.Cart[0].ItemID)
	assert.Equal(t, 1, payload.Cart[0].Quantity)
}

func (s *PlatformIntegrationSuite) assertWebshopGetResponse(body []byte) {
	t := s.T()
	t.Helper()

	var payload struct {
		SupportedCurrencies struct {
			CurrencyCodes []string `json:"currencyCodes"`
		} `json:"supportedCurrencies"`
		ProductsList []struct {
			ID string `json:"id"`
		} `json:"productsList"`
		Ads             []map[string]any `json:"ads"`
		Cart            []map[string]any `json:"cart"`
		Recommendations []map[string]any `json:"recommendations"`
	}
	err := json.Unmarshal(body, &payload)
	require.NoError(t, err, "expected JSON response from webshop get, got: %s", string(body))
	assert.Contains(t, payload.SupportedCurrencies.CurrencyCodes, "USD")
	assert.Contains(t, payload.SupportedCurrencies.CurrencyCodes, "EUR")
	assert.Len(t, payload.ProductsList, 11)
	assert.Len(t, payload.Ads, 2)
	assert.NotNil(t, payload.Cart)
	assert.NotNil(t, payload.Recommendations)
}

func (s *PlatformIntegrationSuite) assertWebshopCartResponse(body []byte, expectedCartSize int, expectedShippingUnits int) {
	t := s.T()
	t.Helper()

	var payload struct {
		Cart         []map[string]any `json:"cart"`
		ShippingCost struct {
			CostUSD struct {
				CurrencyCode string `json:"currencyCode"`
				Units        int    `json:"units"`
				Nanos        int    `json:"nanos"`
			} `json:"costUsd"`
		} `json:"shippingCost"`
	}
	err := json.Unmarshal(body, &payload)
	require.NoError(t, err, "expected JSON response from webshop cart, got: %s", string(body))
	assert.Len(t, payload.Cart, expectedCartSize)
	assert.Equal(t, "USD", payload.ShippingCost.CostUSD.CurrencyCode)
	assert.Equal(t, expectedShippingUnits, payload.ShippingCost.CostUSD.Units)
	assert.Equal(t, 0, payload.ShippingCost.CostUSD.Nanos)
}

func (s *PlatformIntegrationSuite) assertWebshopUserIDResponse(body []byte, userID string) {
	t := s.T()
	t.Helper()

	var payload struct {
		UserID string `json:"userId"`
	}
	err := json.Unmarshal(body, &payload)
	require.NoError(t, err, "expected JSON response from webshop operation, got: %s", string(body))
	assert.Equal(t, userID, payload.UserID)
}

func (s *PlatformIntegrationSuite) waitForFaasdCallgraphEdges(expected []callGraphEdgeExpectation, timeout time.Duration) integrationhelpers.CallGraph {
	t := s.T()
	t.Helper()

	var cg integrationhelpers.CallGraph
	integrationhelpers.Eventually(t, timeout, callgraphPollInterval, func() bool {
		cg = integrationhelpers.GetFaasdCallGraph(t, s.baseURL, s.auth)
		for _, edge := range expected {
			if integrationhelpers.EdgeCount(cg, edge.Caller, edge.Callee) != edge.Count {
				return false
			}
		}
		return true
	})
	return cg
}

func (s *PlatformIntegrationSuite) waitForFaasdCallgraphEdgeDeltas(before integrationhelpers.CallGraph, expected []callGraphEdgeExpectation, timeout time.Duration) integrationhelpers.CallGraph {
	t := s.T()
	t.Helper()

	var cg integrationhelpers.CallGraph
	integrationhelpers.Eventually(t, timeout, callgraphPollInterval, func() bool {
		cg = integrationhelpers.GetFaasdCallGraph(t, s.baseURL, s.auth)
		for _, edge := range expected {
			delta := integrationhelpers.EdgeCount(cg, edge.Caller, edge.Callee) - integrationhelpers.EdgeCount(before, edge.Caller, edge.Callee)
			if delta != edge.Count {
				return false
			}
		}
		return true
	})
	return cg
}

func (s *PlatformIntegrationSuite) assertFaasdFunctionStats(expected []functionStatsExpectation) {
	t := s.T()
	t.Helper()

	for _, fn := range expected {
		var stats integrationhelpers.FunctionCallGraphStats
		// Async descendants can still be finishing after the caller returns,
		// so wait until their callgraph stats settle before asserting.
		integrationhelpers.Eventually(t, callgraphWaitTimeout, callgraphPollInterval, func() bool {
			stats = integrationhelpers.GetFaasdFunctionCallGraphStats(t, s.baseURL, s.auth, fn.Name)
			if stats.Name != fn.Name {
				return false
			}
			if fn.ExactCalls > 0 && stats.TotalCalls != fn.ExactCalls {
				return false
			}
			if fn.ExactCalls == 0 && stats.TotalCalls < fn.MinCalls {
				return false
			}
			if stats.TotalColdStarts < 1 {
				return false
			}
			if stats.TotalPrewarms != 0 {
				return false
			}
			return true
		})

		assert.Equal(t, fn.Name, stats.Name, "expected %s stats name to match", fn.Name)
		if fn.ExactCalls > 0 {
			assert.Equal(t, fn.ExactCalls, stats.TotalCalls, "expected %s total calls to be %d, got %d", fn.Name, fn.ExactCalls, stats.TotalCalls)
		} else {
			assert.GreaterOrEqual(t, stats.TotalCalls, fn.MinCalls, "expected %s total calls to be >= %d, got %d", fn.Name, fn.MinCalls, stats.TotalCalls)
		}
		assert.GreaterOrEqual(t, stats.TotalColdStarts, 1, "expected %s total cold starts to be >=1, got %d", fn.Name, stats.TotalColdStarts)
		assert.Equal(t, 0, stats.TotalPrewarms, "expected %s prewarm count to be 0, got %d", fn.Name, stats.TotalPrewarms)
	}
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

func uniqueTestUserID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}
