package faasd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	integrationhelpers "github.com/mactavishz/FaaS-Platform-Knowledge-Optimization/tests/integration/helpers"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type WorkflowIntegrationSuite struct {
	suite.Suite
	baseURL string
	auth    integrationhelpers.FaasdGatewayAuth
	repo    string
}

func TestWorkflowIntegrationSuite(t *testing.T) {
	suite.Run(t, new(WorkflowIntegrationSuite))
}

func (s *WorkflowIntegrationSuite) SetupSuite() {
	t := s.T()
	s.repo = integrationhelpers.RepoRoot(t)
	s.baseURL, s.auth = integrationhelpers.RequireFaasd(t)
	integrationhelpers.RequireLocalRegistryReachable(t)
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

func (s *WorkflowIntegrationSuite) deployFaasdWorkflow(t *testing.T, stackRelPath string) []string {
	t.Helper()

	stackPath := filepath.Join(s.repo, stackRelPath)
	names := integrationhelpers.FaasdStackFunctionNames(t, stackPath)
	// parallel := integrationhelpers.FaasdStackFunctionCount(t, stackPath)

	os.Setenv("REGISTRY_PREFIX", "macsalvation/faasd-")
	integrationhelpers.RemoveFaasdWorkflowStack(t, s.baseURL, stackPath)
	t.Cleanup(func() {
		integrationhelpers.RemoveFaasdWorkflowStack(t, s.baseURL, stackPath)
		os.Unsetenv("REGISTRY_PREFIX")
	})

	integrationhelpers.DeployFaasdWorkflowStack(t, s.baseURL, stackPath)
	integrationhelpers.WaitForFaasdFunctionsPresent(t, s.baseURL, s.auth, names, 120*time.Second)

	return names
}

func (s *WorkflowIntegrationSuite) runLinear3Batch() {
	t := s.T()
	s.deployFaasdWorkflow(t, "tests/workflows/faasd/linear3/stack.yaml")
	body := integrationhelpers.InvokeFaasdJSON(t, s.baseURL, s.auth, "linear3-a", map[string]any{})

	type linear3Payload struct {
		Msg            string         `json:"msg"`
		RequestHeaders map[string]any `json:"request_headers"`
		Linear3BBody   struct {
			Msg            string         `json:"msg"`
			RequestHeaders map[string]any `json:"request_headers"`
			Linear3CBody   struct {
				Msg            string         `json:"msg"`
				RequestHeaders map[string]any `json:"request_headers"`
			} `json:"linear3-c-body"`
		} `json:"linear3-b-body"`
	}

	var payload linear3Payload
	integrationhelpers.DecodeJSON(t, body, &payload)

	if payload.Msg == "" {
		var wrapped struct {
			Body linear3Payload `json:"body"`
		}
		integrationhelpers.DecodeJSON(t, body, &wrapped)
		payload = wrapped.Body
	}

	require.Equal(t, "Function linear3-a is finished", payload.Msg)
	require.Equal(t, "Function linear3-b is finished", payload.Linear3BBody.Msg)
	require.Equal(t, "Function linear3-c is finished", payload.Linear3BBody.Linear3CBody.Msg)
	require.NotEmpty(t, payload.RequestHeaders)
	require.NotEmpty(t, payload.Linear3BBody.RequestHeaders)
	require.NotEmpty(t, payload.Linear3BBody.Linear3CBody.RequestHeaders)
}

func (s *WorkflowIntegrationSuite) runTreeBatch() {
	t := s.T()
	s.deployFaasdWorkflow(t, "tests/workflows/faasd/tree/stack.yaml")
	body := integrationhelpers.InvokeFaasdJSON(t, s.baseURL, s.auth, "tree-a", map[string]any{"traceId": "tree-it"})

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
}

func (s *WorkflowIntegrationSuite) runIoTBatch() {
	t := s.T()
	integrationhelpers.RequireWorkflowEnvFile(t, "tests/workflows/faasd/IoT/.env.yaml")
	s.deployFaasdWorkflow(t, "tests/workflows/faasd/IoT/stack.yaml")
	entryBody := integrationhelpers.InvokeFaasdJSON(t, s.baseURL, s.auth, "iot-i", map[string]any{})
	var entry struct {
		Results []map[string]any `json:"results"`
	}
	integrationhelpers.DecodeJSON(t, entryBody, &entry)
	require.Len(t, entry.Results, 3)
	for _, item := range entry.Results {
		require.Empty(t, item)
	}

	cwBody := integrationhelpers.InvokeFaasdJSON(t, s.baseURL, s.auth, "iot-cw", map[string]any{"sieve": 1000})
	var cw struct {
		Valid        bool  `json:"valid"`
		TimeMs       int64 `json:"time"`
		Eratosthenes []int `json:"eratosthenes"`
	}
	integrationhelpers.DecodeJSON(t, cwBody, &cw)
	require.True(t, cw.Valid)
	require.GreaterOrEqual(t, cw.TimeMs, int64(0))
	require.NotEmpty(t, cw.Eratosthenes)

	csBody := integrationhelpers.InvokeFaasdJSON(t, s.baseURL, s.auth, "iot-cs", map[string]any{
		"originalEvent": map[string]any{"sensorID": 42, "value": 10},
	})
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

	caBody := integrationhelpers.InvokeFaasdJSON(t, s.baseURL, s.auth, "iot-ca", map[string]any{
		"originalEvent": map[string]any{"sensorID": 42, "value": 10},
	})
	var ca struct {
		From string `json:"from"`
	}
	integrationhelpers.DecodeJSON(t, caBody, &ca)
	require.Equal(t, "CheckAir", ca.From)
}

func (s *WorkflowIntegrationSuite) runWebshopBatch() {
	t := s.T()
	integrationhelpers.RequireWorkflowEnvFile(t, "tests/workflows/faasd/webshop/.env.yaml")
	s.deployFaasdWorkflow(t, "tests/workflows/faasd/webshop/stack.yaml")
	t.Run("get", func(t *testing.T) {
		body := integrationhelpers.InvokeFaasdJSON(t, s.baseURL, s.auth, "webshop-frontend", map[string]any{
			"operation": "get",
			"userId":    "it-webshop-get",
			"currency":  "USD",
		})

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
		emptyWebshopCart(t, s.baseURL, s.auth, userID)

		addBody := integrationhelpers.InvokeFaasdJSON(t, s.baseURL, s.auth, "webshop-frontend", map[string]any{
			"operation": "addcart",
			"userId":    userID,
			"productId": "3",
			"quantity":  2,
		})

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

		cartBody := integrationhelpers.InvokeFaasdJSON(t, s.baseURL, s.auth, "webshop-frontend", map[string]any{
			"operation": "cart",
			"userId":    userID,
		})

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
		emptyWebshopCart(t, s.baseURL, s.auth, userID)

		_ = integrationhelpers.InvokeFaasdJSON(t, s.baseURL, s.auth, "webshop-frontend", map[string]any{
			"operation": "addcart",
			"userId":    userID,
			"productId": "3",
			"quantity":  1,
		})

		checkoutBody := integrationhelpers.InvokeFaasdJSON(t, s.baseURL, s.auth, "webshop-frontend", map[string]any{
			"operation": "checkout",
			"userId":    userID,
			"currency":  "EUR",
			"address": map[string]any{
				"street": "123 Main St",
			},
		})

		var checkoutResp struct {
			UserID string `json:"userId"`
		}
		integrationhelpers.DecodeJSON(t, checkoutBody, &checkoutResp)
		require.Equal(t, userID, checkoutResp.UserID)

		integrationhelpers.Eventually(t, 120*time.Second, 3*time.Second, func() bool {
			status, body, err := integrationhelpers.InvokeFaasdJSONOnce(t, s.baseURL, s.auth, "webshop-frontend", map[string]any{
				"operation": "cart",
				"userId":    userID,
			})
			if err != nil || status != 200 {
				return false
			}
			var cartResp struct {
				Cart []map[string]any `json:"cart"`
			}
			integrationhelpers.DecodeJSON(t, body, &cartResp)
			return len(cartResp.Cart) == 0
		})
	})
}

func emptyWebshopCart(t *testing.T, baseURL string, auth integrationhelpers.FaasdGatewayAuth, userID string) {
	t.Helper()
	_ = integrationhelpers.InvokeFaasdJSON(t, baseURL, auth, "webshop-frontend", map[string]any{
		"operation": "emptycart",
		"userId":    userID,
	})

	integrationhelpers.Eventually(t, 120*time.Second, 3*time.Second, func() bool {
		status, body, err := integrationhelpers.InvokeFaasdJSONOnce(t, baseURL, auth, "webshop-frontend", map[string]any{
			"operation": "cart",
			"userId":    userID,
		})
		if err != nil || status != 200 {
			return false
		}
		var cartResp struct {
			Cart []map[string]any `json:"cart"`
		}
		integrationhelpers.DecodeJSON(t, body, &cartResp)
		return len(cartResp.Cart) == 0
	})
}
