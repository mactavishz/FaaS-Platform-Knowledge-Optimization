package tinyfaas

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"testing"
	"time"

	integrationhelpers "github.com/mactavishz/FaaS-Platform-Knowledge-Optimization/tests/integration/helpers"
)

const (
	iotStackPath       = "tests/workflows/tinyfaas/IoT/stack.yaml"
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
	integrationhelpers.EnsureTinyFaaSVM(t)

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
	t.Logf("[profile] rebuilding tinyFaaS profile=%s env=%s", name, envFile)

	integrationhelpers.RebuildTinyFaaSWithEnvFile(t, envFile)
	integrationhelpers.WaitForGateway(t, integrationhelpers.DEFAULT_TINYFAAS_GATEWAY_URL, 90*time.Second)
	integrationhelpers.WipeFunctions(t)
	t.Cleanup(func() { integrationhelpers.WipeFunctions(t) })
	integrationhelpers.DeployWorkflow(t, iotStackPath)

	samples := make([]time.Duration, 0, 10)
	for i := 0; i < 10; i++ {
		t.Logf("[profile:%s] iteration=%d waiting for scale-down", name, i+1)
		integrationhelpers.WaitForFunctionsScaledDown(t, iotFunctions, 2*time.Minute)

		beforeAS := getSuccessfulInvocations(t, "iot-as")
		start := time.Now()
		status, body, err := integrationhelpers.InvokeJSONOnce("iot-i", map[string]any{}, performanceTimeout)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("profile=%s iteration=%d invoke failed: %v", name, i+1, err)
		}
		if status != http.StatusOK {
			t.Fatalf("profile=%s iteration=%d expected status=200 got=%d body=%s", name, i+1, status, string(body))
		}
		validateIoTEntryResponse(t, body)
		waitForSuccessfulInvocations(t, "iot-as", beforeAS+2, 2*time.Minute)

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

func waitForSuccessfulInvocations(t *testing.T, functionName string, min int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if got := getSuccessfulInvocations(t, functionName); got >= min {
			return
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatalf("timed out waiting for %s successful invocations >= %d, got %d", functionName, min, getSuccessfulInvocations(t, functionName))
}

func getSuccessfulInvocations(t *testing.T, functionName string) int {
	t.Helper()
	url := fmt.Sprintf("%s/system/stats/function/%s", integrationhelpers.DEFAULT_TINYFAAS_GATEWAY_URL, functionName)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Get(url)
	if err != nil {
		t.Fatalf("get function stats %s: %v", functionName, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return 0
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("function stats %s failed: status=%d body=%s", functionName, resp.StatusCode, string(body))
	}
	var decoded struct {
		Summary struct {
			SuccessfulInvocations int `json:"successful_invocations"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode function stats %s: %v body=%s", functionName, err, string(body))
	}
	return decoded.Summary.SuccessfulInvocations
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
