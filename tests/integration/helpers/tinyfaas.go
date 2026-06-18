package helpers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

const (
	DEFAULT_TINYFAAS_GATEWAY_URL     = "http://127.0.0.1:8080"
	TINYFAAS_LINEAR3_STACK_FILE_PATH = "tests/workflows/tinyfaas/linear3/stack.yaml"
	NO_AUTOSCALER_PROFILE            = "no-autoscaler-no-callgraph.env"
)

type FunctionStatus struct {
	Name    string `json:"name"`
	Running bool   `json:"running"`
}

func EnsureTinyFaaSVM(t *testing.T) {
	t.Helper()
	t.Log("[step] verifying tinyfaas VM is running")
	EnsureVagrantVMExclusive(t, "tinyfaas", "faasd")
}

func RequireTinyFaaS(t *testing.T) string {
	t.Helper()
	EnsureTinyFaaSVM(t)
	WaitForGateway(t, DEFAULT_TINYFAAS_GATEWAY_URL, 90*time.Second)
	return DEFAULT_TINYFAAS_GATEWAY_URL
}

func RebuildTinyFaaS(t *testing.T, envProfile string) {
	t.Helper()
	t.Logf("[step] rebuilding tinyFaaS with profile=%s", envProfile)

	RebuildTinyFaaSWithEnvFile(t, "/vagrant/tests/integration/env/"+envProfile)
}

func RebuildTinyFaaSWithEnvFile(t *testing.T, envFile string) {
	t.Helper()
	t.Logf("[step] rebuilding tinyFaaS with env=%s", envFile)

	cmd := fmt.Sprintf("PROJECT_ROOT=/vagrant ENV_FILE=%s bash /vagrant/scripts/build-tinyfaas.sh", envFile)
	MustRunCommand(t, CommandOptions{Timeout: 35 * time.Minute, Dir: RepoRoot(t)}, "vagrant", "ssh", "tinyfaas", "-c", cmd)
	t.Log("[step] tinyFaaS rebuild complete")
}

func WaitForGateway(t *testing.T, baseURL string, timeout time.Duration) {
	t.Helper()
	t.Logf("[step] waiting for gateway=%s health", baseURL)

	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := client.Get(strings.TrimRight(baseURL, "/") + "/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				t.Log("[step] gateway healthy")
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	t.Fatalf("gateway not healthy at %s within %v", baseURL, timeout)
}

func WipeFunctions(t *testing.T) {
	t.Helper()
	t.Log("[step] wiping all functions via /system/wipe")

	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Post(DEFAULT_TINYFAAS_GATEWAY_URL+"/system/wipe", "application/json", nil)
	if err != nil {
		t.Fatalf("failed to wipe functions: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("wipe failed: status=%d body=%s", resp.StatusCode, string(body))
	}
	t.Log("[step] wipe completed")
}

func DeployWorkflow(t *testing.T, stackPath string) {
	DeployWorkflowWithEnvs(t, stackPath, nil)
}

func DeployWorkflowWithEnvs(t *testing.T, stackPath string, envs map[string]string) {
	t.Helper()
	t.Logf("[step] deploying workflow stack=%s", stackPath)

	stack := filepath.Join(RepoRoot(t), stackPath)
	args := []string{
		"deploy",
		"--platform", "tinyfaas",
		"--gateway", DEFAULT_TINYFAAS_GATEWAY_URL,
		"-f", stack,
	}

	if len(envs) > 0 {
		keys := make([]string, 0, len(envs))
		for k := range envs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			args = append(args, "-e", fmt.Sprintf("%s=%s", k, envs[k]))
		}
	}

	MustRunCommand(t, CommandOptions{Timeout: 10 * time.Minute, Dir: RepoRoot(t)},
		"faas-cli",
		args...,
	)
	t.Logf("[step] workflow deployed stack=%s", stackPath)
}

func DeployTinyFaaSStackFilterWithEnvs(t *testing.T, gateway string, stackPath string, filter string, envs map[string]string) {
	t.Helper()
	args := []string{
		"deploy",
		"--platform", "tinyfaas",
		"--gateway", gateway,
		"-f", stackPath,
		"--filter", filter,
	}

	MustRunCommand(t, CommandOptions{Timeout: 10 * time.Minute, Dir: RepoRoot(t), Env: envs}, "faas-cli", args...)
}

func DeployTinyFaaSStackFilter(t *testing.T, gateway string, stackPath string, filter string) {
	t.Helper()
	DeployTinyFaaSStackFilterWithEnvs(t, gateway, stackPath, filter, nil)
}

func RemoveTinyFaaSStackFilter(t *testing.T, gateway string, stackPath string, filter string) {
	t.Helper()
	_, _ = TryRunCommand(
		t,
		CommandOptions{Timeout: 2 * time.Minute, Dir: RepoRoot(t)},
		"faas-cli",
		"remove",
		"--platform", "tinyfaas",
		"--gateway", gateway,
		"-f", stackPath,
		"--filter", filter,
	)
}

func SetupFunctionalityScenario(t *testing.T, stackPath string, functionNames []string) {
	t.Helper()
	t.Logf("[setup] functionality stack=%s profile=%s", stackPath, NO_AUTOSCALER_PROFILE)

	EnsureTinyFaaSVM(t)
	RebuildTinyFaaS(t, NO_AUTOSCALER_PROFILE)
	WaitForGateway(t, DEFAULT_TINYFAAS_GATEWAY_URL, 90*time.Second)
	WipeFunctions(t)
	t.Cleanup(func() { WipeFunctions(t) })
	DeployWorkflow(t, stackPath)
	WaitForFunctionsRunningState(t, functionNames, true, 90*time.Second)
}

func InvokeFunction(t *testing.T, functionName string, payload string, timeout time.Duration) []byte {
	t.Helper()
	t.Logf("[step] invoking %s timeout=%s payload_bytes=%d", functionName, timeout, len(payload))

	status, body, err := InvokeFunctionOnce(functionName, payload, timeout)
	if err != nil {
		t.Fatalf("invoke %s failed: %v", functionName, err)
	}
	if status != http.StatusOK {
		t.Fatalf("invoke %s failed, expected status=%d got status=%d body=%s", functionName, http.StatusOK, status, string(body))
	}
	if len(body) == 0 {
		t.Fatalf("invoke %s returned empty body", functionName)
	}

	return body
}

func InvokeFunctionOnce(functionName string, payload string, timeout time.Duration) (int, []byte, error) {
	url := DEFAULT_TINYFAAS_GATEWAY_URL + "/fn/" + functionName
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBufferString(payload))
	if err != nil {
		return 0, nil, err
	}

	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body, nil
}

func InvokeTinyFaaS(t *testing.T, baseURL string, functionName string, method string, body []byte, headers map[string]string) (int, []byte) {
	t.Helper()

	url := strings.TrimRight(baseURL, "/") + "/fn/" + functionName
	return invokeTinyFaaSURL(t, url, method, body, headers)
}

func InvokeTinyFaaSAsync(t *testing.T, baseURL string, functionName string, method string, body []byte, headers map[string]string) (int, []byte) {
	t.Helper()

	url := strings.TrimRight(baseURL, "/") + "/async-fn/" + functionName
	return invokeTinyFaaSURL(t, url, method, body, headers)
}

func invokeTinyFaaSURL(t *testing.T, url string, method string, body []byte, headers map[string]string) (int, []byte) {
	t.Helper()

	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create invoke request: %v", err)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("Content-Type") == "" && len(body) > 0 {
		req.Header.Set("Content-Type", "text/plain")
	}

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("invoke request failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read invoke response: %v", err)
	}

	return resp.StatusCode, respBody
}

func TinyFaaSSystemList(t *testing.T, baseURL string) []byte {
	t.Helper()

	url := strings.TrimRight(baseURL, "/") + "/system/list"
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Get(url)
	if err != nil {
		t.Fatalf("failed to list functions: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read list response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list functions failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	return body
}

func InvokeJSONOnce(functionName string, payload any, timeout time.Duration) (int, []byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal payload: %w", err)
	}

	url := DEFAULT_TINYFAAS_GATEWAY_URL + "/fn/" + functionName
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, respBody, nil
}

func InvokeJSON(t *testing.T, functionName string, payload any, timeout time.Duration) []byte {
	t.Helper()
	t.Logf("[step] invoking %s with JSON payload timeout=%s", functionName, timeout)

	status, body, err := InvokeJSONOnce(functionName, payload, timeout)
	if err != nil {
		t.Fatalf("invoke %s failed: %v", functionName, err)
	}
	if status != http.StatusOK {
		t.Fatalf("invoke %s failed, expected status=%d got status=%d body=%s", functionName, http.StatusOK, status, string(body))
	}
	if len(body) == 0 {
		t.Fatalf("invoke %s returned empty body", functionName)
	}

	return body
}

func ListFunctions(t *testing.T) []FunctionStatus {
	t.Helper()

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Get(DEFAULT_TINYFAAS_GATEWAY_URL + "/system/list")
	if err != nil {
		t.Fatalf("failed to list functions: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list functions failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	var out []FunctionStatus
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("failed to decode /system/list response: %v", err)
	}
	return out
}

func WaitForFunctionsRunningState(t *testing.T, names []string, running bool, timeout time.Duration) {
	t.Helper()
	t.Logf("[step] waiting for running=%t names=%v timeout=%s", running, names, timeout)

	deadline := time.Now().Add(timeout)
	attempt := 0
	for time.Now().Before(deadline) {
		attempt++
		if HasRunningState(ListFunctions(t), names, running) {
			t.Logf("[step] running=%t names=%v reached after checks=%d", running, names, attempt)
			return
		}
		if attempt%5 == 0 {
			t.Logf("[step] still waiting for running=%t names=%v", running, names)
		}
		time.Sleep(2 * time.Second)
	}

	t.Fatalf("timed out waiting for running=%t for functions %v", running, names)
}

func WaitForFunctionsScaledDown(t *testing.T, names []string, timeout time.Duration) {
	t.Helper()
	WaitForFunctionsRunningState(t, names, false, timeout)
}

// ScaleDownTinyFaaS forces a function (or "*" for all functions) to scale to
// zero via the /system/scale-down endpoint, bypassing the autoscaler's idle
// timer. The call blocks until the scale-down completes, so functions are
// already scaled down when it returns 200.
func ScaleDownTinyFaaS(t *testing.T, name string) {
	t.Helper()
	url := DEFAULT_TINYFAAS_GATEWAY_URL + "/system/scale-down/" + name
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Post(url, "application/json", nil)
	if err != nil {
		t.Fatalf("scale-down POST failed for %q: %v", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("scale-down %q expected 200, got %d body=%s", name, resp.StatusCode, string(body))
	}
}

func WaitForFunctionsPresent(t *testing.T, names []string, timeout time.Duration) {
	t.Helper()
	t.Logf("[step] waiting for functions present names=%v timeout=%s", names, timeout)

	deadline := time.Now().Add(timeout)
	attempt := 0
	for time.Now().Before(deadline) {
		attempt++
		functions := ListFunctions(t)
		present := true
		index := make(map[string]struct{}, len(functions))
		for _, fn := range functions {
			index[fn.Name] = struct{}{}
		}
		for _, name := range names {
			if _, ok := index[name]; !ok {
				present = false
				break
			}
		}

		if present {
			t.Logf("[step] functions present names=%v reached after checks=%d", names, attempt)
			return
		}

		if attempt%5 == 0 {
			t.Logf("[step] still waiting for functions present names=%v", names)
		}
		time.Sleep(2 * time.Second)
	}

	t.Fatalf("timed out waiting for functions present %v", names)
}

func HasRunningState(functions []FunctionStatus, names []string, running bool) bool {
	index := make(map[string]bool, len(functions))
	for _, fn := range functions {
		index[fn.Name] = fn.Running
	}

	for _, name := range names {
		state, ok := index[name]
		if !ok || state != running {
			return false
		}
	}

	return true
}

func GetCallGraph(t *testing.T) CallGraph {
	t.Helper()
	t.Log("[step] fetching callgraph")

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Get(DEFAULT_TINYFAAS_GATEWAY_URL + "/system/callgraph")
	if err != nil {
		t.Fatalf("failed to get callgraph: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("callgraph request failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	var cg CallGraph
	if err := json.Unmarshal(body, &cg); err != nil {
		t.Fatalf("failed to decode callgraph response: %v", err)
	}
	return cg
}

func GetFunctionCallGraphStats(t *testing.T, functionName string) FunctionCallGraphStats {
	t.Helper()
	t.Logf("[step] fetching callgraph function stats function=%s", functionName)

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Get(DEFAULT_TINYFAAS_GATEWAY_URL + "/system/callgraph/function/" + functionName)
	if err != nil {
		t.Fatalf("failed to get callgraph function stats for %s: %v", functionName, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("callgraph function stats request failed for %s: status=%d body=%s", functionName, resp.StatusCode, string(body))
	}

	var stats FunctionCallGraphStats
	if err := json.Unmarshal(body, &stats); err != nil {
		t.Fatalf("failed to decode callgraph function stats for %s: %v", functionName, err)
	}

	return stats
}
