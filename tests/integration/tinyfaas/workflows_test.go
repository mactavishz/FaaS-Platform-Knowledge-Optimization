package tinyfaas

import (
	"testing"
	"time"

	integrationhelpers "github.com/mactavishz/FaaS-Platform-Knowledge-Optimization/tests/integration/helpers"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type WorkflowIntegrationSuite struct {
	suite.Suite
}

func TestWorkflowIntegrationSuite(t *testing.T) {
	suite.Run(t, new(WorkflowIntegrationSuite))
}

func (s *WorkflowIntegrationSuite) SetupSuite() {
	t := s.T()
	integrationhelpers.EnsureTinyFaaSVM(t)
	integrationhelpers.RebuildTinyFaaS(t, integrationhelpers.NO_AUTOSCALER_PROFILE)
	integrationhelpers.WaitForGateway(t, integrationhelpers.DEFAULT_TINYFAAS_GATEWAY_URL, 90*time.Second)
	integrationhelpers.WipeFunctions(t)
	t.Cleanup(func() { integrationhelpers.WipeFunctions(t) })
}

func (s *WorkflowIntegrationSuite) TestWorkflows() {
	t := s.T()
	t.Run("linear3", func(t *testing.T) {
		s.runLinear3Batch()
	})
	t.Run("tree", func(t *testing.T) {
		s.runTreeBatch()
	})
	t.Run("iot", func(t *testing.T) {
		s.runIoTBatch()
	})
	t.Run("webshop", func(t *testing.T) {
		s.runWebshopBatch()
	})
}

func (s *WorkflowIntegrationSuite) runLinear3Batch() {
	t := s.T()
	integrationhelpers.WipeFunctions(t)
	integrationhelpers.DeployWorkflow(t, "tests/workflows/tinyfaas/linear3/stack.yaml")
	integrationhelpers.WaitForFunctionsRunningState(t, []string{"linear3-a", "linear3-b", "linear3-c"}, true, 90*time.Second)

	body := integrationhelpers.InvokeJSON(t, "linear3-a", map[string]any{}, 120*time.Second)

	var payload struct {
		Msg            string         `json:"msg"`
		RequestHeaders map[string]any `json:"request_headers"`
		Data           struct {
			Msg            string         `json:"msg"`
			RequestHeaders map[string]any `json:"request_headers"`
			Data           struct {
				Msg            string         `json:"msg"`
				Data           string         `json:"data"`
				RequestHeaders map[string]any `json:"request_headers"`
			} `json:"data"`
		} `json:"data"`
	}
	integrationhelpers.DecodeJSON(t, body, &payload)

	require.Equal(t, "Function linear3-a is finished", payload.Msg)
	require.Equal(t, "Function linear3-b is finished", payload.Data.Msg)
	require.Equal(t, "Function linear3-c is finished", payload.Data.Data.Msg)
	require.Equal(t, "Hello World! End of the story.", payload.Data.Data.Data)
	require.NotEmpty(t, payload.RequestHeaders)
	require.NotEmpty(t, payload.Data.RequestHeaders)
	require.NotEmpty(t, payload.Data.Data.RequestHeaders)

	integrationhelpers.WipeFunctions(t)
}

func (s *WorkflowIntegrationSuite) runTreeBatch() {
	t := s.T()
	integrationhelpers.WipeFunctions(t)
	integrationhelpers.DeployWorkflow(t, "tests/workflows/tinyfaas/tree/stack.yaml")
	integrationhelpers.WaitForFunctionsRunningState(t, []string{"tree-a", "tree-b", "tree-c", "tree-d", "tree-e", "tree-f", "tree-g"}, true, 90*time.Second)

	body := integrationhelpers.InvokeJSON(t, "tree-a", map[string]any{"traceId": "tree-it"}, 120*time.Second)

	var payload struct {
		From    string           `json:"from"`
		TraceID string           `json:"traceId"`
		Results []map[string]any `json:"results"`
		Checked []struct {
			From    string           `json:"from"`
			Checked []map[string]any `json:"checked"`
		} `json:"checked"`
	}
	integrationhelpers.DecodeJSON(t, body, &payload)

	require.Equal(t, "A", payload.From)
	require.Equal(t, "tree-it", payload.TraceID)
	require.Len(t, payload.Results, 1)
	require.Empty(t, payload.Results[0])

	require.Len(t, payload.Checked, 1)
	require.Equal(t, "B", payload.Checked[0].From)
	require.Len(t, payload.Checked[0].Checked, 2)

	leafFrom := map[string]bool{}
	for _, leaf := range payload.Checked[0].Checked {
		name, ok := leaf["from"].(string)
		require.True(t, ok)
		leafFrom[name] = true
	}
	require.True(t, leafFrom["D"])
	require.True(t, leafFrom["E"])

	integrationhelpers.WipeFunctions(t)
}

func (s *WorkflowIntegrationSuite) runIoTBatch() {
	t := s.T()
	integrationhelpers.RequireWorkflowSupabaseEnv(t)
	integrationhelpers.WipeFunctions(t)
	integrationhelpers.DeployWorkflow(t, "tests/workflows/tinyfaas/IoT/stack.yaml")
	integrationhelpers.WaitForFunctionsRunningState(t, []string{"iot-i", "iot-as", "iot-ca", "iot-cs", "iot-csa", "iot-csl", "iot-ct", "iot-cw", "iot-dj", "iot-se"}, true, 90*time.Second)

	entryBody := integrationhelpers.InvokeJSON(t, "iot-i", map[string]any{}, 120*time.Second)
	var entry struct {
		Results []map[string]any `json:"results"`
	}
	integrationhelpers.DecodeJSON(t, entryBody, &entry)
	require.Len(t, entry.Results, 3)
	for _, item := range entry.Results {
		require.Empty(t, item)
	}

	cwBody := integrationhelpers.InvokeJSON(t, "iot-cw", map[string]any{"sieve": 1000}, 60*time.Second)
	var cw struct {
		Valid        bool  `json:"valid"`
		TimeMs       int64 `json:"time"`
		Eratosthenes []int `json:"eratosthenes"`
	}
	integrationhelpers.DecodeJSON(t, cwBody, &cw)
	require.True(t, cw.Valid)
	require.GreaterOrEqual(t, cw.TimeMs, int64(0))
	require.NotEmpty(t, cw.Eratosthenes)

	csBody := integrationhelpers.InvokeJSON(t, "iot-cs", map[string]any{
		"originalEvent": map[string]any{"sensorID": 42, "value": 10},
	}, 90*time.Second)
	var cs struct {
		From  string `json:"from"`
		Calls []struct {
			From string `json:"from"`
		} `json:"calls"`
	}
	integrationhelpers.DecodeJSON(t, csBody, &cs)
	require.Equal(t, "CheckSound", cs.From)
	require.Len(t, cs.Calls, 2)

	foundLoud := false
	foundAccident := false
	for _, c := range cs.Calls {
		if c.From == "CheckSoundLoud" {
			foundLoud = true
		}
		if c.From == "CheckSoundAccident" {
			foundAccident = true
		}
	}
	require.True(t, foundLoud)
	require.True(t, foundAccident)

	caBody := integrationhelpers.InvokeJSON(t, "iot-ca", map[string]any{
		"originalEvent": map[string]any{"sensorID": 42, "value": 10},
	}, 90*time.Second)
	var ca struct {
		From string `json:"from"`
	}
	integrationhelpers.DecodeJSON(t, caBody, &ca)
	require.Equal(t, "CheckAir", ca.From)

	integrationhelpers.WipeFunctions(t)
}

func (s *WorkflowIntegrationSuite) runWebshopBatch() {
	t := s.T()
	integrationhelpers.RequireWorkflowSupabaseEnv(t)
	integrationhelpers.WipeFunctions(t)
	integrationhelpers.DeployWorkflow(t, "tests/workflows/tinyfaas/webshop/stack.yaml")
	integrationhelpers.WaitForFunctionsRunningState(t, []string{
		"webshop-frontend", "webshop-checkout", "webshop-addcartitem", "webshop-emptycart", "webshop-getcart",
		"webshop-cartstorage", "webshop-listproducts", "webshop-getproduct", "webshop-searchproducts",
		"webshop-listrecommendations", "webshop-currency", "webshop-supportedcurrencies", "webshop-getads",
		"webshop-shipmentquote", "webshop-shiporder", "webshop-payment", "webshop-email",
	}, true, 90*time.Second)

	t.Run("get", func(t *testing.T) {
		body := integrationhelpers.InvokeJSON(t, "webshop-frontend", map[string]any{
			"operation": "get",
			"userId":    "it-webshop-get",
			"currency":  "USD",
		}, 120*time.Second)

		var payload struct {
			SupportedCurrencies struct {
				CurrencyCodes []string `json:"currencyCodes"`
			} `json:"supportedCurrencies"`
			ProductsList []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"productsList"`
			Ads             []map[string]any `json:"ads"`
			Cart            []map[string]any `json:"cart"`
			Recommendations []map[string]any `json:"recommendations"`
		}
		integrationhelpers.DecodeJSON(t, body, &payload)

		require.Contains(t, payload.SupportedCurrencies.CurrencyCodes, "USD")
		require.Contains(t, payload.SupportedCurrencies.CurrencyCodes, "EUR")
		require.Len(t, payload.ProductsList, 11)

		foundKnownProduct := false
		for _, p := range payload.ProductsList {
			if p.ID == "1" && p.Name == "T-Shirt" {
				foundKnownProduct = true
			}
		}
		require.True(t, foundKnownProduct)
		require.Len(t, payload.Ads, 2)
		require.NotNil(t, payload.Cart)
		require.NotNil(t, payload.Recommendations)
	})

	t.Run("addcart_and_cart", func(t *testing.T) {
		userID := "it-webshop-cart"
		emptyWebshopCart(t, userID)

		addBody := integrationhelpers.InvokeJSON(t, "webshop-frontend", map[string]any{
			"operation": "addcart",
			"userId":    userID,
			"productId": "3",
			"quantity":  2,
		}, 120*time.Second)

		var addResp struct {
			Cart []struct {
				ItemID   string `json:"itemId"`
				UserID   string `json:"userId"`
				Quantity int    `json:"quantity"`
			} `json:"cart"`
		}
		integrationhelpers.DecodeJSON(t, addBody, &addResp)
		require.Len(t, addResp.Cart, 1)
		require.Equal(t, "3", addResp.Cart[0].ItemID)
		require.Equal(t, userID, addResp.Cart[0].UserID)
		require.Equal(t, 2, addResp.Cart[0].Quantity)

		cartBody := integrationhelpers.InvokeJSON(t, "webshop-frontend", map[string]any{
			"operation": "cart",
			"userId":    userID,
		}, 120*time.Second)

		var cartResp struct {
			Cart []struct {
				ItemID   string `json:"itemId"`
				UserID   string `json:"userId"`
				Quantity int    `json:"quantity"`
			} `json:"cart"`
			ShippingCost struct {
				CostUSD struct {
					CurrencyCode string `json:"currencyCode"`
					Units        int    `json:"units"`
					Nanos        int    `json:"nanos"`
				} `json:"costUsd"`
			} `json:"shippingCost"`
		}
		integrationhelpers.DecodeJSON(t, cartBody, &cartResp)
		require.Len(t, cartResp.Cart, 1)
		require.Equal(t, "USD", cartResp.ShippingCost.CostUSD.CurrencyCode)
		require.Equal(t, 3, cartResp.ShippingCost.CostUSD.Units)
		require.Equal(t, 0, cartResp.ShippingCost.CostUSD.Nanos)
	})

	t.Run("checkout", func(t *testing.T) {
		userID := "it-webshop-checkout"
		emptyWebshopCart(t, userID)

		_ = integrationhelpers.InvokeJSON(t, "webshop-frontend", map[string]any{
			"operation": "addcart",
			"userId":    userID,
			"productId": "3",
			"quantity":  1,
		}, 120*time.Second)

		checkoutBody := integrationhelpers.InvokeJSON(t, "webshop-frontend", map[string]any{
			"operation": "checkout",
			"userId":    userID,
			"currency":  "EUR",
			"address": map[string]any{
				"street": "123 Main St",
			},
		}, 150*time.Second)

		var checkoutResp struct {
			UserID string `json:"userId"`
		}
		integrationhelpers.DecodeJSON(t, checkoutBody, &checkoutResp)
		require.Equal(t, userID, checkoutResp.UserID)

		integrationhelpers.Eventually(t, 120*time.Second, 3*time.Second, func() bool {
			body := integrationhelpers.InvokeJSON(t, "webshop-frontend", map[string]any{
				"operation": "cart",
				"userId":    userID,
			}, 30*time.Second)
			var cartResp struct {
				Cart []map[string]any `json:"cart"`
			}
			integrationhelpers.DecodeJSON(t, body, &cartResp)
			return len(cartResp.Cart) == 0
		})
	})

	integrationhelpers.WipeFunctions(t)
}

func emptyWebshopCart(t *testing.T, userID string) {
	t.Helper()
	_ = integrationhelpers.InvokeJSON(t, "webshop-frontend", map[string]any{
		"operation": "emptycart",
		"userId":    userID,
	}, 90*time.Second)

	integrationhelpers.Eventually(t, 120*time.Second, 3*time.Second, func() bool {
		body := integrationhelpers.InvokeJSON(t, "webshop-frontend", map[string]any{
			"operation": "cart",
			"userId":    userID,
		}, 30*time.Second)
		var cartResp struct {
			Cart []map[string]any `json:"cart"`
		}
		integrationhelpers.DecodeJSON(t, body, &cartResp)
		return len(cartResp.Cart) == 0
	})
}
