package faasd

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"sort"
	"testing"
	"time"

	integrationhelpers "github.com/mactavishz/FaaS-Platform-Knowledge-Optimization/tests/integration/helpers"
)

const (
	iotStackRelPath    = "tests/workflows/faasd/IoT/stack.yaml"
	performanceTimeout = 120 * time.Second
)

var iotFunctions = []string{"iot-i", "iot-as", "iot-ca", "iot-cs", "iot-csa", "iot-csl", "iot-ct", "iot-cw", "iot-dj", "iot-se"}

type profileResult struct {
	name    string
	samples []time.Duration
	median  time.Duration
}

func TestIoTOptimizedProfilesReduceUserLatency(t *testing.T) {
	integrationhelpers.RequireWorkflowSupabaseEnv(t)
	integrationhelpers.EnsureFaasdVM(t)

	profiles := []struct {
		name string
		env  string
	}{
		{name: "baseline", env: "/vagrant/benchmark/env/baseline.env"},
		{name: "optimized-sma", env: "/vagrant/benchmark/env/optimized-sma.env"},
		{name: "optimized-ema", env: "/vagrant/benchmark/env/optimized-ema.env"},
	}

	results := make([]profileResult, 0, len(profiles))
	for _, profile := range profiles {
		result := runIoTProfile(t, profile.name, profile.env)
		results = append(results, result)
		t.Logf("[result] profile=%s median=%s samples=%v", result.name, result.median, result.samples)
	}

	baseline := results[0]
	for _, result := range results[1:] {
		if result.median >= baseline.median {
			t.Fatalf("%s median %s must be lower than baseline median %s\nbaseline samples=%v\n%s samples=%v",
				result.name, result.median, baseline.median, baseline.samples, result.name, result.samples)
		}
	}
}

func runIoTProfile(t *testing.T, name string, envFile string) profileResult {
	t.Helper()
	t.Logf("[profile] rebuilding faasd profile=%s env=%s", name, envFile)

	integrationhelpers.RebuildFaasdWithEnvFile(t, envFile)
	baseURL, auth := integrationhelpers.RequireFaasd(t)

	stackPath := filepath.Join(integrationhelpers.RepoRoot(t), iotStackRelPath)
	integrationhelpers.RemoveFaasdWorkflowStack(t, baseURL, stackPath)
	t.Cleanup(func() { integrationhelpers.RemoveFaasdWorkflowStack(t, baseURL, stackPath) })
	integrationhelpers.PrepareFaasdWorkflowStack(t, stackPath)
	integrationhelpers.DeployFaasdWorkflowStack(t, baseURL, stackPath)
	integrationhelpers.WaitForFaasdFunctionsPresent(t, baseURL, auth, iotFunctions, performanceTimeout)

	samples := make([]time.Duration, 0, 10)
	for i := 0; i < 10; i++ {
		t.Logf("[profile:%s] iteration=%d waiting for scale-down", name, i+1)
		waitForFaasdFunctionsScaledDown(t, baseURL, auth, iotFunctions, 2*time.Minute)

		beforeAS := getFaasdTotalCalls(t, baseURL, auth, "iot-as")
		start := time.Now()
		status, body, err := integrationhelpers.InvokeFaasdJSONOnceWithTimeout(t, baseURL, auth, "iot-i", map[string]any{}, performanceTimeout)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("profile=%s iteration=%d invoke failed: %v", name, i+1, err)
		}
		if status != http.StatusOK {
			t.Fatalf("profile=%s iteration=%d expected status=200 got=%d body=%s", name, i+1, status, string(body))
		}
		validateIoTEntryResponse(t, body)
		waitForFaasdTotalCalls(t, baseURL, auth, "iot-as", beforeAS+2, 2*time.Minute)

		samples = append(samples, elapsed)
		t.Logf("[profile:%s] iteration=%d latency=%s", name, i+1, elapsed)
	}

	return profileResult{name: name, samples: samples, median: medianDuration(samples)}
}

func validateIoTEntryResponse(t *testing.T, body []byte) {
	t.Helper()
	var payload struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode iot response: %v body=%s", err, string(body))
	}
	if len(payload.Results) != 3 {
		t.Fatalf("expected 3 async placeholders, got %d body=%s", len(payload.Results), string(body))
	}
	for i, item := range payload.Results {
		if len(item) != 0 {
			t.Fatalf("expected empty async placeholder at index %d, got %#v", i, item)
		}
	}
}

func waitForFaasdFunctionsScaledDown(t *testing.T, baseURL string, auth integrationhelpers.FaasdGatewayAuth, functions []string, timeout time.Duration) {
	t.Helper()

	integrationhelpers.Eventually(t, timeout, 1*time.Second, func() bool {
		for _, fn := range functions {
			status := integrationhelpers.GetFaasdFunction(t, baseURL, auth, fn)
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

func waitForFaasdTotalCalls(t *testing.T, baseURL string, auth integrationhelpers.FaasdGatewayAuth, functionName string, min int, timeout time.Duration) {
	t.Helper()

	integrationhelpers.Eventually(t, timeout, 1*time.Second, func() bool {
		return getFaasdTotalCalls(t, baseURL, auth, functionName) >= min
	})
}

func getFaasdTotalCalls(t *testing.T, baseURL string, auth integrationhelpers.FaasdGatewayAuth, functionName string) int {
	t.Helper()
	stats := integrationhelpers.GetFaasdFunctionCallGraphStats(t, baseURL, auth, functionName)
	return stats.TotalCalls
}

func medianDuration(samples []time.Duration) time.Duration {
	sorted := append([]time.Duration(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}
