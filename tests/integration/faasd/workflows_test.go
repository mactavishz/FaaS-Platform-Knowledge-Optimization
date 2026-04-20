package faasd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type WorkflowIntegrationSuite struct {
	suite.Suite
	baseURL string
	auth    gatewayAuth
	repo    string
}

func TestWorkflowIntegrationSuite(t *testing.T) {
	suite.Run(t, new(WorkflowIntegrationSuite))
}

func (s *WorkflowIntegrationSuite) SetupSuite() {
	t := s.T()
	s.repo = repoRoot(t)
	s.baseURL, s.auth = requireFaasd(t)
	requireLocalRegistryReachable(t)
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
	names := stackFunctionNames(t, stackPath)
	// parallel := stackFunctionCount(t, stackPath)

	os.Setenv("REGISTRY_PREFIX", "macsalvation/faasd-")
	removeWorkflowStack(t, s.baseURL, stackPath)
	t.Cleanup(func() {
		removeWorkflowStack(t, s.baseURL, stackPath)
		os.Unsetenv("REGISTRY_PREFIX")
	})

	// buildWorkflowStack(t, stackPath)
	// pushWorkflowStack(t, stackPath, parallel)
	deployWorkflowStack(t, s.baseURL, stackPath)
	waitForFunctionsPresent(t, s.baseURL, s.auth, names, 120*time.Second)

	return names
}

func (s *WorkflowIntegrationSuite) runLinear3Batch() {
	t := s.T()
	names := s.deployFaasdWorkflow(t, "tests/workflows/faasd/linear3/stack.yaml")
	warmFunctions(t, s.baseURL, s.auth, excludeFunction(names, "linear3-a"), 90*time.Second)

	body := invokeJSONEventually(t, s.baseURL, s.auth, "linear3-a", map[string]any{}, 120*time.Second)

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
	decodeJSON(t, body, &payload)

	if payload.Msg == "" {
		var wrapped struct {
			Body linear3Payload `json:"body"`
		}
		decodeJSON(t, body, &wrapped)
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
	names := s.deployFaasdWorkflow(t, "tests/workflows/faasd/tree/stack.yaml")
	warmFunctions(t, s.baseURL, s.auth, excludeFunction(names, "tree-a"), 90*time.Second)

	body := invokeJSONEventually(t, s.baseURL, s.auth, "tree-a", map[string]any{"traceId": "tree-it"}, 120*time.Second)

	var payload struct {
		From    string           `json:"from"`
		TraceID string           `json:"traceId"`
		Results []map[string]any `json:"results"`
		Checked []struct {
			From    string           `json:"from"`
			Checked []map[string]any `json:"checked"`
		} `json:"checked"`
	}
	decodeJSON(t, body, &payload)

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
	requireWorkflowEnvFile(t, filepath.Join(s.repo, "tests/workflows/faasd/IoT/.env.yaml"))
	names := s.deployFaasdWorkflow(t, "tests/workflows/faasd/IoT/stack.yaml")
	warmFunctions(t, s.baseURL, s.auth, excludeFunction(names, "iot-i"), 90*time.Second)

	entryBody := invokeJSONEventually(t, s.baseURL, s.auth, "iot-i", map[string]any{}, 120*time.Second)
	var entry struct {
		Results []map[string]any `json:"results"`
	}
	decodeJSON(t, entryBody, &entry)
	require.Len(t, entry.Results, 3)
	for _, item := range entry.Results {
		require.Empty(t, item)
	}

	cwBody := invokeJSONEventually(t, s.baseURL, s.auth, "iot-cw", map[string]any{"sieve": 1000}, 60*time.Second)
	var cw struct {
		Valid        bool  `json:"valid"`
		TimeMs       int64 `json:"time"`
		Eratosthenes []int `json:"eratosthenes"`
	}
	decodeJSON(t, cwBody, &cw)
	require.True(t, cw.Valid)
	require.GreaterOrEqual(t, cw.TimeMs, int64(0))
	require.NotEmpty(t, cw.Eratosthenes)

	csBody := invokeJSONEventually(t, s.baseURL, s.auth, "iot-cs", map[string]any{
		"originalEvent": map[string]any{"sensorID": 42, "value": 10},
	}, 90*time.Second)
	var cs struct {
		From  string `json:"from"`
		Calls []struct {
			From string `json:"from"`
		} `json:"calls"`
	}
	decodeJSON(t, csBody, &cs)
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

	caBody := invokeJSONEventually(t, s.baseURL, s.auth, "iot-ca", map[string]any{
		"originalEvent": map[string]any{"sensorID": 42, "value": 10},
	}, 90*time.Second)
	var ca struct {
		From string `json:"from"`
	}
	decodeJSON(t, caBody, &ca)
	require.Equal(t, "CheckAir", ca.From)
}

func (s *WorkflowIntegrationSuite) runWebshopBatch() {
	t := s.T()
	requireWorkflowEnvFile(t, filepath.Join(s.repo, "tests/workflows/faasd/webshop/.env.yaml"))
	names := s.deployFaasdWorkflow(t, "tests/workflows/faasd/webshop/stack.yaml")
	warmFunctions(t, s.baseURL, s.auth, excludeFunction(names, "webshop-frontend"), 90*time.Second)

	t.Run("get", func(t *testing.T) {
		body := invokeJSONEventually(t, s.baseURL, s.auth, "webshop-frontend", map[string]any{
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
		decodeJSON(t, body, &payload)

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

		addBody := invokeJSONEventually(t, s.baseURL, s.auth, "webshop-frontend", map[string]any{
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
		decodeJSON(t, addBody, &addResp)
		require.Len(t, addResp.Cart, 1)
		require.Equal(t, "3", addResp.Cart[0].ItemID)
		require.Equal(t, userID, addResp.Cart[0].UserID)
		require.Equal(t, 2, addResp.Cart[0].Quantity)

		cartBody := invokeJSONEventually(t, s.baseURL, s.auth, "webshop-frontend", map[string]any{
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
		decodeJSON(t, cartBody, &cartResp)
		require.Len(t, cartResp.Cart, 1)
		require.Equal(t, "USD", cartResp.ShippingCost.CostUSD.CurrencyCode)
		require.Equal(t, 3, cartResp.ShippingCost.CostUSD.Units)
		require.Equal(t, 0, cartResp.ShippingCost.CostUSD.Nanos)
	})

	t.Run("checkout", func(t *testing.T) {
		userID := "it-webshop-checkout"
		emptyWebshopCart(t, s.baseURL, s.auth, userID)

		_ = invokeJSONEventually(t, s.baseURL, s.auth, "webshop-frontend", map[string]any{
			"operation": "addcart",
			"userId":    userID,
			"productId": "3",
			"quantity":  1,
		}, 120*time.Second)

		checkoutBody := invokeJSONEventually(t, s.baseURL, s.auth, "webshop-frontend", map[string]any{
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
		decodeJSON(t, checkoutBody, &checkoutResp)
		require.Equal(t, userID, checkoutResp.UserID)

		eventually(t, 120*time.Second, 3*time.Second, func() bool {
			body := invokeJSONEventually(t, s.baseURL, s.auth, "webshop-frontend", map[string]any{
				"operation": "cart",
				"userId":    userID,
			}, 30*time.Second)
			var cartResp struct {
				Cart []map[string]any `json:"cart"`
			}
			decodeJSON(t, body, &cartResp)
			return len(cartResp.Cart) == 0
		})
	})
}

func emptyWebshopCart(t *testing.T, baseURL string, auth gatewayAuth, userID string) {
	t.Helper()
	_ = invokeJSONEventually(t, baseURL, auth, "webshop-frontend", map[string]any{
		"operation": "emptycart",
		"userId":    userID,
	}, 90*time.Second)

	eventually(t, 120*time.Second, 3*time.Second, func() bool {
		body := invokeJSONEventually(t, baseURL, auth, "webshop-frontend", map[string]any{
			"operation": "cart",
			"userId":    userID,
		}, 30*time.Second)
		var cartResp struct {
			Cart []map[string]any `json:"cart"`
		}
		decodeJSON(t, body, &cartResp)
		return len(cartResp.Cart) == 0
	})
}

func excludeFunction(names []string, excluded string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		if name == excluded {
			continue
		}
		out = append(out, name)
	}
	return out
}
