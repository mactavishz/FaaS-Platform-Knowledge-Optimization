package tinyfaas

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	integrationhelpers "github.com/mactavishz/FaaS-Platform-Knowledge-Optimization/tests/integration/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

const (
	tinyFaaSTreeStackPath       = "tests/workflows/tinyfaas/tree/stack.yaml"
	tinyFaaSIoTStackPath        = "tests/workflows/tinyfaas/IoT/stack.yaml"
	tinyFaaSIoTEnvPath          = "tests/workflows/tinyfaas/IoT/.env.yaml"
	tinyFaaSWebshopStackPath    = "tests/workflows/tinyfaas/webshop/stack.yaml"
	tinyFaaSWebshopEnvPath      = "tests/workflows/tinyfaas/webshop/.env.yaml"
	tinyCallgraphWaitTimeout    = 120 * time.Second
	tinyCallgraphPollInterval   = 1 * time.Second
	tinyWorkflowTimeout         = 120 * time.Second
	tinyWebshopCheckoutTimeout  = 180 * time.Second
	tinyWorkflowResponseTimeout = 90 * time.Second
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
}

func TestPlatformIntegrationSuite(t *testing.T) {
	suite.Run(t, new(PlatformIntegrationSuite))
}

func (s *PlatformIntegrationSuite) setupScenario(envProfile string, stackPath string, functionNames []string, deployEnvs map[string]string) {
	t := s.T()
	t.Logf("[setup] profile=%s stack=%s", envProfile, stackPath)
	integrationhelpers.RequireTinyFaaSVM(t)
	integrationhelpers.RebuildTinyFaaS(t, envProfile)
	integrationhelpers.WaitForGateway(t, integrationhelpers.DEFAULT_TINYFAAS_GATEWAY_URL, 90*time.Second)
	integrationhelpers.WipeFunctions(t)
	t.Cleanup(func() { integrationhelpers.WipeFunctions(t) })
	integrationhelpers.DeployWorkflowWithEnvs(t, stackPath, deployEnvs)
	integrationhelpers.WaitForFunctionsPresent(t, functionNames, 60*time.Second)
}

func (s *PlatformIntegrationSuite) TestNoAutoscalerNoCallgraph() {
	t := s.T()
	t.Log("[scenario] no autoscaler + no callgraph")
	s.setupScenario(
		"no-autoscaler-no-callgraph.env",
		integrationhelpers.TINYFAAS_LINEAR3_STACK_FILE_PATH,
		[]string{"linear3-a", "linear3-b", "linear3-c"},
		nil,
	)

	body := integrationhelpers.InvokeFunction(t, "linear3-a", "integration-test", tinyWorkflowResponseTimeout)
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
	s.setupScenario(
		"autoscaler-only.env",
		integrationhelpers.TINYFAAS_LINEAR3_STACK_FILE_PATH,
		[]string{"linear3-a", "linear3-b", "linear3-c"},
		nil,
	)

	body := integrationhelpers.InvokeFunction(t, "linear3-a", "integration-test", tinyWorkflowResponseTimeout)
	s.assertWorkflowResponse(body)

	// Idle timeout is 12s + autoscaler check interval is 10s.
	integrationhelpers.WaitForFunctionsScaledDown(t, []string{"linear3-a", "linear3-b", "linear3-c"}, 90*time.Second)

	body = integrationhelpers.InvokeFunction(t, "linear3-a", "integration-test", tinyWorkflowTimeout)
	s.assertWorkflowResponse(body)

	s.assertCallgraphEmpty()
}

func (s *PlatformIntegrationSuite) TestAutoscalerAndCallgraphNoPrewarm() {
	t := s.T()
	t.Log("[scenario] autoscaler + callgraph + prewarm disabled")
	s.setupScenario(
		"autoscaler-and-callgraph-no-prewarm.env",
		integrationhelpers.TINYFAAS_LINEAR3_STACK_FILE_PATH,
		[]string{"linear3-a", "linear3-b", "linear3-c"},
		map[string]string{"FUNCTION_DELAY_SEC": "5"},
	)

	body := integrationhelpers.InvokeFunction(t, "linear3-a", "integration-test", tinyWorkflowResponseTimeout)
	s.assertWorkflowResponse(body)

	s.waitForCallgraphEdges([]callGraphEdgeExpectation{
		{Caller: "", Callee: "linear3-a", Count: 1},
		{Caller: "linear3-a", Callee: "linear3-b", Count: 1},
		{Caller: "linear3-b", Callee: "linear3-c", Count: 1},
	}, tinyCallgraphWaitTimeout)
	s.assertFunctionStats([]functionStatsExpectation{
		{Name: "linear3-a", ExactCalls: 1},
		{Name: "linear3-b", ExactCalls: 1},
		{Name: "linear3-c", ExactCalls: 1},
	})
}

func (s *PlatformIntegrationSuite) TestAutoscalerAndCallgraphNoPrewarmTree() {
	t := s.T()
	t.Log("[scenario] autoscaler + callgraph + prewarm disabled tree")
	s.setupScenario(
		"autoscaler-and-callgraph-no-prewarm.env",
		tinyFaaSTreeStackPath,
		[]string{"tree-a", "tree-b", "tree-c", "tree-d", "tree-e", "tree-f", "tree-g"},
		nil,
	)

	traceID := "tree-platform"
	body := integrationhelpers.InvokeJSON(t, "tree-a", map[string]any{"traceId": traceID}, tinyWorkflowTimeout)
	s.assertTreeResponse(body, traceID)

	s.waitForCallgraphEdges([]callGraphEdgeExpectation{
		{Caller: "", Callee: "tree-a", Count: 1},
		{Caller: "tree-a", Callee: "tree-b", Count: 1},
		{Caller: "tree-a", Callee: "tree-c", Count: 1},
		{Caller: "tree-b", Callee: "tree-d", Count: 1},
		{Caller: "tree-b", Callee: "tree-e", Count: 1},
		{Caller: "tree-c", Callee: "tree-f", Count: 1},
		{Caller: "tree-c", Callee: "tree-g", Count: 1},
	}, tinyCallgraphWaitTimeout)
	s.assertFunctionStats([]functionStatsExpectation{
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
	t.Log("[scenario] autoscaler + callgraph + prewarm disabled iot")
	integrationhelpers.RequireWorkflowEnvFile(t, tinyFaaSIoTEnvPath)
	s.setupScenario(
		"autoscaler-and-callgraph-no-prewarm.env",
		tinyFaaSIoTStackPath,
		[]string{"iot-i", "iot-as", "iot-ca", "iot-cs", "iot-csa", "iot-csl", "iot-ct", "iot-cw", "iot-dj", "iot-se"},
		nil,
	)

	body := integrationhelpers.InvokeJSON(t, "iot-i", map[string]any{}, tinyWorkflowTimeout)
	s.assertIoTResponse(body)

	s.waitForCallgraphEdges([]callGraphEdgeExpectation{
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
	}, tinyCallgraphWaitTimeout)
	s.assertFunctionStats([]functionStatsExpectation{
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
	t.Log("[scenario] autoscaler + callgraph + prewarm disabled webshop")
	integrationhelpers.RequireWorkflowEnvFile(t, tinyFaaSWebshopEnvPath)
	s.setupScenario(
		"autoscaler-and-callgraph-no-prewarm.env",
		tinyFaaSWebshopStackPath,
		[]string{
			"webshop-frontend", "webshop-checkout", "webshop-addcartitem", "webshop-emptycart", "webshop-getcart",
			"webshop-cartstorage", "webshop-listproducts", "webshop-getproduct", "webshop-searchproducts",
			"webshop-listrecommendations", "webshop-currency", "webshop-supportedcurrencies", "webshop-getads",
			"webshop-shipmentquote", "webshop-shiporder", "webshop-payment", "webshop-email",
		},
		nil,
	)

	userID := uniqueTestUserID("platform-webshop")
	baseline := integrationhelpers.GetCallGraph(t)
	getBody := integrationhelpers.InvokeJSON(t, "webshop-frontend", map[string]any{
		"operation": "get",
		"userId":    userID,
		"currency":  "USD",
	}, tinyWorkflowTimeout)
	s.assertWebshopGetResponse(getBody)
	s.waitForCallgraphEdgeDeltas(baseline, []callGraphEdgeExpectation{
		{Caller: "", Callee: "webshop-frontend", Count: 1},
		{Caller: "webshop-frontend", Callee: "webshop-supportedcurrencies", Count: 1},
		{Caller: "webshop-frontend", Callee: "webshop-listproducts", Count: 1},
		{Caller: "webshop-frontend", Callee: "webshop-currency", Count: 11},
		{Caller: "webshop-frontend", Callee: "webshop-getads", Count: 1},
		{Caller: "webshop-frontend", Callee: "webshop-getcart", Count: 1},
		{Caller: "webshop-getcart", Callee: "webshop-cartstorage", Count: 1},
		{Caller: "webshop-frontend", Callee: "webshop-listrecommendations", Count: 1},
		{Caller: "webshop-listrecommendations", Callee: "webshop-listproducts", Count: 1},
	}, tinyCallgraphWaitTimeout)

	baseline = integrationhelpers.GetCallGraph(t)
	cartBody := integrationhelpers.InvokeJSON(t, "webshop-frontend", map[string]any{
		"operation": "cart",
		"userId":    userID,
	}, tinyWorkflowTimeout)
	s.assertWebshopCartResponse(cartBody, 0, 0)
	s.waitForCallgraphEdgeDeltas(baseline, []callGraphEdgeExpectation{
		{Caller: "", Callee: "webshop-frontend", Count: 1},
		{Caller: "webshop-frontend", Callee: "webshop-getcart", Count: 1},
		{Caller: "webshop-getcart", Callee: "webshop-cartstorage", Count: 1},
		{Caller: "webshop-frontend", Callee: "webshop-shipmentquote", Count: 1},
	}, tinyCallgraphWaitTimeout)

	baseline = integrationhelpers.GetCallGraph(t)
	addCartBody := integrationhelpers.InvokeJSON(t, "webshop-frontend", map[string]any{
		"operation": "addcart",
		"userId":    userID,
		"productId": "3",
		"quantity":  1,
	}, tinyWorkflowTimeout)
	s.assertWebshopAddCartResponse(addCartBody, userID)
	s.waitForCallgraphEdgeDeltas(baseline, []callGraphEdgeExpectation{
		{Caller: "", Callee: "webshop-frontend", Count: 1},
		{Caller: "webshop-frontend", Callee: "webshop-addcartitem", Count: 1},
		{Caller: "webshop-addcartitem", Callee: "webshop-cartstorage", Count: 1},
		{Caller: "webshop-frontend", Callee: "webshop-getcart", Count: 1},
		{Caller: "webshop-getcart", Callee: "webshop-cartstorage", Count: 1},
	}, tinyCallgraphWaitTimeout)

	baseline = integrationhelpers.GetCallGraph(t)
	checkoutBody := integrationhelpers.InvokeJSON(t, "webshop-frontend", map[string]any{
		"operation": "checkout",
		"userId":    userID,
		"currency":  "EUR",
		"address": map[string]any{
			"street": "123 Main St",
		},
	}, tinyWebshopCheckoutTimeout)
	s.assertWebshopUserIDResponse(checkoutBody, userID)

	s.waitForCallgraphEdgeDeltas(baseline, []callGraphEdgeExpectation{
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
	}, tinyCallgraphWaitTimeout)

	baseline = integrationhelpers.GetCallGraph(t)
	emptyCartBody := integrationhelpers.InvokeJSON(t, "webshop-frontend", map[string]any{
		"operation": "emptycart",
		"userId":    userID,
	}, tinyWorkflowTimeout)
	s.assertWebshopUserIDResponse(emptyCartBody, userID)
	s.waitForCallgraphEdgeDeltas(baseline, []callGraphEdgeExpectation{
		{Caller: "", Callee: "webshop-frontend", Count: 1},
		{Caller: "webshop-frontend", Callee: "webshop-emptycart", Count: 1},
		{Caller: "webshop-emptycart", Callee: "webshop-cartstorage", Count: 1},
	}, tinyCallgraphWaitTimeout)

	s.assertFunctionStats([]functionStatsExpectation{
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
	t.Log("[scenario] autoscaler + callgraph + prewarm enabled")
	s.setupScenario(
		"autoscaler-and-callgraph-and-prewarm.env",
		integrationhelpers.TINYFAAS_LINEAR3_STACK_FILE_PATH,
		[]string{"linear3-a", "linear3-b", "linear3-c"},
		map[string]string{"FUNCTION_DELAY_SEC": "5"},
	)

	body := integrationhelpers.InvokeFunction(t, "linear3-a", "integration-test", tinyWorkflowResponseTimeout)
	s.assertWorkflowResponse(body)

	s.waitForCallgraphEdges([]callGraphEdgeExpectation{
		{Caller: "", Callee: "linear3-a", Count: 1},
		{Caller: "linear3-a", Callee: "linear3-b", Count: 1},
		{Caller: "linear3-b", Callee: "linear3-c", Count: 1},
	}, tinyCallgraphWaitTimeout)
	statsBBefore := integrationhelpers.GetFunctionCallGraphStats(t, "linear3-b")
	statsCBefore := integrationhelpers.GetFunctionCallGraphStats(t, "linear3-c")

	integrationhelpers.WaitForFunctionsScaledDown(t, []string{"linear3-a", "linear3-b", "linear3-c"}, 90*time.Second)

	body = integrationhelpers.InvokeFunction(t, "linear3-a", "integration-test", tinyWebshopCheckoutTimeout)
	s.assertWorkflowResponse(body)

	s.waitForCallgraphEdges([]callGraphEdgeExpectation{
		{Caller: "", Callee: "linear3-a", Count: 2},
		{Caller: "linear3-a", Callee: "linear3-b", Count: 2},
		{Caller: "linear3-b", Callee: "linear3-c", Count: 2},
	}, tinyCallgraphWaitTimeout)
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

func (s *PlatformIntegrationSuite) waitForCallgraphEdges(expected []callGraphEdgeExpectation, timeout time.Duration) integrationhelpers.CallGraph {
	t := s.T()
	t.Helper()

	var cg integrationhelpers.CallGraph
	integrationhelpers.Eventually(t, timeout, tinyCallgraphPollInterval, func() bool {
		cg = integrationhelpers.GetCallGraph(t)
		for _, edge := range expected {
			if integrationhelpers.EdgeCount(cg, edge.Caller, edge.Callee) != edge.Count {
				return false
			}
		}
		return true
	})
	return cg
}

func (s *PlatformIntegrationSuite) waitForCallgraphEdgeDeltas(before integrationhelpers.CallGraph, expected []callGraphEdgeExpectation, timeout time.Duration) integrationhelpers.CallGraph {
	t := s.T()
	t.Helper()

	var cg integrationhelpers.CallGraph
	integrationhelpers.Eventually(t, timeout, tinyCallgraphPollInterval, func() bool {
		cg = integrationhelpers.GetCallGraph(t)
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

func (s *PlatformIntegrationSuite) assertFunctionStats(expected []functionStatsExpectation) {
	t := s.T()
	t.Helper()

	for _, fn := range expected {
		stats := integrationhelpers.GetFunctionCallGraphStats(t, fn.Name)
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

func uniqueTestUserID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}
