package helpers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const DefaultGatewayURL = "http://127.0.0.1:8888"

const (
	Linear3StackPath    = "tests/workflows/tinyfaas/linear3/stack.yaml"
	NoAutoscalerProfile = "no-autoscaler-no-callgraph.env"
)

type FunctionStatus struct {
	Name    string `json:"name"`
	Running bool   `json:"running"`
}

type EdgeStats struct {
	Caller string `json:"caller"`
	Callee string `json:"callee"`
	Count  int    `json:"count"`
}

type CallGraph struct {
	Edges     []EdgeStats    `json:"edges"`
	Functions map[string]any `json:"functions"`
}

func RepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current working directory: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "Vagrantfile")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("failed to locate repository root from %s", dir)
		}
		dir = parent
	}
}

func MustCommand(t *testing.T, timeout time.Duration, dir string, name string, args ...string) string {
	t.Helper()
	t.Logf("[cmd:start] %s %s", name, strings.Join(args, " "))
	start := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %s %s\nerr: %v\noutput:\n%s", name, strings.Join(args, " "), err, string(out))
	}
	t.Logf("[cmd:done] %s %s (%s)", name, strings.Join(args, " "), time.Since(start).Round(time.Millisecond))

	return string(out)
}

func RequireTinyFaaSVM(t *testing.T) {
	t.Helper()
	t.Log("[step] verifying tinyfaas VM is running")

	if _, err := exec.LookPath("vagrant"); err != nil {
		t.Skip("vagrant not found in PATH")
	}

	out := MustCommand(t, 30*time.Second, RepoRoot(t), "vagrant", "status", "tinyfaas", "--machine-readable")
	if !strings.Contains(out, ",tinyfaas,state,running") {
		t.Fatalf("tinyfaas VM is not running. Run: vagrant up tinyfaas")
	}
}

func RebuildTinyFaaS(t *testing.T, envProfile string) {
	t.Helper()
	t.Logf("[step] rebuilding tinyFaaS with profile=%s", envProfile)

	cmd := fmt.Sprintf("PROJECT_ROOT=/vagrant TF_ENV_FILE=/vagrant/tests/integration/env/%s bash /vagrant/scripts/build-tinyfaas.sh", envProfile)
	MustCommand(t, 35*time.Minute, RepoRoot(t), "vagrant", "ssh", "tinyfaas", "-c", cmd)
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

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Post(DefaultGatewayURL+"/system/wipe", "application/json", nil)
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
	t.Helper()
	t.Logf("[step] deploying workflow stack=%s", stackPath)

	stack := filepath.Join(RepoRoot(t), stackPath)
	MustCommand(t, 10*time.Minute, RepoRoot(t),
		"faas-cli",
		"deploy",
		"--platform", "tinyfaas",
		"--gateway", DefaultGatewayURL,
		"-f", stack,
	)
	t.Logf("[step] workflow deployed stack=%s", stackPath)
}

func SetupFunctionalityScenario(t *testing.T, stackPath string, functionNames []string) {
	t.Helper()
	t.Logf("[setup] functionality stack=%s profile=%s", stackPath, NoAutoscalerProfile)

	RequireTinyFaaSVM(t)
	RebuildTinyFaaS(t, NoAutoscalerProfile)
	WaitForGateway(t, DefaultGatewayURL, 90*time.Second)
	WipeFunctions(t)
	t.Cleanup(func() { WipeFunctions(t) })
	DeployWorkflow(t, stackPath)
	WaitForFunctionsReady(t, functionNames, true, 90*time.Second)
}

func InvokeFunctionEventually(t *testing.T, functionName string, timeout time.Duration) []byte {
	t.Helper()
	return InvokeFunctionEventuallyWithPayload(t, functionName, "integration-test", timeout)
}

func InvokeFunctionEventuallyWithPayload(t *testing.T, functionName string, payload string, timeout time.Duration) []byte {
	t.Helper()
	t.Logf("[step] invoking %s with retries timeout=%s payload_bytes=%d", functionName, timeout, len(payload))

	deadline := time.Now().Add(timeout)
	attempt := 0
	for time.Now().Before(deadline) {
		attempt++
		status, body, err := InvokeFunctionOnce(functionName, payload, 20*time.Second)
		if err == nil && status == http.StatusOK && len(body) > 0 {
			t.Logf("[step] invoke %s succeeded at attempt=%d", functionName, attempt)
			return body
		}

		if err != nil {
			t.Logf("[invoke %s attempt=%d] err=%v", functionName, attempt, err)
		} else {
			t.Logf("[invoke %s attempt=%d] status=%d body=%s", functionName, attempt, status, string(body))
		}
		time.Sleep(2 * time.Second)
	}

	t.Fatalf("invoke %s did not succeed within %s", functionName, timeout)
	return nil
}

func InvokeFunctionOnce(functionName string, payload string, timeout time.Duration) (int, []byte, error) {
	url := DefaultGatewayURL + "/fn/" + functionName
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

func InvokeJSONOnce(functionName string, payload any, timeout time.Duration) (int, []byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal payload: %w", err)
	}

	url := DefaultGatewayURL + "/fn/" + functionName
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

func InvokeJSONEventually(t *testing.T, functionName string, payload any, timeout time.Duration) []byte {
	t.Helper()
	t.Logf("[step] invoking %s with JSON payload timeout=%s", functionName, timeout)

	deadline := time.Now().Add(timeout)
	attempt := 0
	for time.Now().Before(deadline) {
		attempt++
		status, body, err := InvokeJSONOnce(functionName, payload, 20*time.Second)
		if err == nil && status == http.StatusOK && len(body) > 0 {
			t.Logf("[step] invoke %s succeeded at attempt=%d", functionName, attempt)
			return body
		}

		if err != nil {
			t.Logf("[invoke %s attempt=%d] err=%v", functionName, attempt, err)
		} else {
			t.Logf("[invoke %s attempt=%d] status=%d body=%s", functionName, attempt, status, string(body))
		}
		time.Sleep(2 * time.Second)
	}

	t.Fatalf("invoke %s did not succeed within %s", functionName, timeout)
	return nil
}

func DecodeJSON[T any](t *testing.T, body []byte, out *T) {
	t.Helper()
	if err := json.Unmarshal(body, out); err != nil {
		t.Fatalf("failed to decode JSON response: %v body=%s", err, string(body))
	}
}

func RequireWorkflowEnvFile(t *testing.T, relPath string) {
	t.Helper()
	absPath := filepath.Join(RepoRoot(t), relPath)

	content, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("required workflow env file missing or unreadable: %s err=%v", absPath, err)
	}

	text := string(content)
	if strings.TrimSpace(text) == "" {
		t.Fatalf("required workflow env file is empty: %s", absPath)
	}

	if !strings.Contains(text, "SUPABASE_URL") || !strings.Contains(text, "SUPABASE_KEY") {
		t.Fatalf("required workflow env file must define SUPABASE_URL and SUPABASE_KEY: %s", absPath)
	}

	if strings.Contains(text, "your_supabase_project_url") || strings.Contains(text, "your_supabase_publishable_key") {
		t.Fatalf("required workflow env file still contains placeholder values: %s", absPath)
	}
}

func Eventually(t *testing.T, timeout time.Duration, interval time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(interval)
	}
	t.Fatalf("condition not met within %s", timeout)
}

func ListFunctions(t *testing.T) []FunctionStatus {
	t.Helper()

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Get(DefaultGatewayURL + "/system/list")
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

func WaitForFunctionsReady(t *testing.T, names []string, running bool, timeout time.Duration) {
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

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Get(DefaultGatewayURL + "/system/callgraph")
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

func EdgeCount(cg CallGraph, caller string, callee string) int {
	for _, e := range cg.Edges {
		if e.Caller == caller && e.Callee == callee {
			return e.Count
		}
	}
	return 0
}
