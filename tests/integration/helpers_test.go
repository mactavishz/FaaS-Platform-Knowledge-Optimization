package integration

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

const (
	defaultGatewayURL = "http://127.0.0.1:8888"
	linear3StackPath  = "tests/workflows/tinyfaas/linear3/stack.yaml"
)

type functionStatus struct {
	Name    string `json:"name"`
	Running bool   `json:"running"`
}

type edgeStats struct {
	Caller string `json:"caller"`
	Callee string `json:"callee"`
	Count  int    `json:"count"`
}

type callGraph struct {
	Edges     []edgeStats    `json:"edges"`
	Functions map[string]any `json:"functions"`
}

func repoRoot(t *testing.T) string {
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

func mustCommand(t *testing.T, timeout time.Duration, dir string, name string, args ...string) string {
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

func requireTinyFaaSVM(t *testing.T) {
	t.Helper()
	t.Log("[step] verifying tinyfaas VM is running")

	if _, err := exec.LookPath("vagrant"); err != nil {
		t.Skip("vagrant not found in PATH")
	}

	out := mustCommand(t, 30*time.Second, repoRoot(t), "vagrant", "status", "tinyfaas", "--machine-readable")
	if !strings.Contains(out, ",tinyfaas,state,running") {
		t.Fatalf("tinyfaas VM is not running. Run: vagrant up tinyfaas")
	}
}

func rebuildTinyFaaS(t *testing.T, envProfile string) {
	t.Helper()
	t.Logf("[step] rebuilding tinyFaaS with profile=%s", envProfile)

	cmd := fmt.Sprintf("PROJECT_ROOT=/vagrant TF_ENV_FILE=/vagrant/tests/integration/env/%s bash /vagrant/scripts/build-tinyfaas.sh", envProfile)
	mustCommand(t, 35*time.Minute, repoRoot(t), "vagrant", "ssh", "tinyfaas", "-c", cmd)
	t.Log("[step] tinyFaaS rebuild complete")
}

func waitForGateway(t *testing.T, baseURL string, timeout time.Duration) {
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

func wipeFunctions(t *testing.T) {
	t.Helper()
	t.Log("[step] wiping all functions via /system/wipe")

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Post(defaultGatewayURL+"/system/wipe", "application/json", nil)
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

func deployLinear3Workflow(t *testing.T) {
	t.Helper()
	t.Log("[step] deploying linear3 workflow")

	stack := filepath.Join(repoRoot(t), linear3StackPath)
	mustCommand(t, 5*time.Minute, repoRoot(t),
		"faas-cli",
		"deploy",
		"--platform", "tinyfaas",
		"--gateway", defaultGatewayURL,
		"-f", stack,
	)
	t.Log("[step] linear3 workflow deployed")
}

func invokeFunctionEventually(t *testing.T, functionName string, timeout time.Duration) []byte {
	t.Helper()
	return invokeFunctionEventuallyWithPayload(t, functionName, "integration-test", timeout)
}

func invokeFunctionEventuallyWithPayload(t *testing.T, functionName string, payload string, timeout time.Duration) []byte {
	t.Helper()
	t.Logf("[step] invoking %s with retries timeout=%s payload_bytes=%d", functionName, timeout, len(payload))

	deadline := time.Now().Add(timeout)
	attempt := 0
	for time.Now().Before(deadline) {
		attempt++
		status, body, err := invokeFunctionOnce(functionName, payload, 20*time.Second)
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

func invokeFunctionOnce(functionName string, payload string, timeout time.Duration) (int, []byte, error) {
	url := defaultGatewayURL + "/fn/" + functionName
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

func listFunctions(t *testing.T) []functionStatus {
	t.Helper()

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Get(defaultGatewayURL + "/system/list")
	if err != nil {
		t.Fatalf("failed to list functions: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list functions failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	var out []functionStatus
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("failed to decode /system/list response: %v", err)
	}
	return out
}

func waitForFunctionsReady(t *testing.T, names []string, running bool, timeout time.Duration) {
	t.Helper()
	t.Logf("[step] waiting for running=%t names=%v timeout=%s", running, names, timeout)

	deadline := time.Now().Add(timeout)
	attempt := 0
	for time.Now().Before(deadline) {
		attempt++
		if hasRunningState(listFunctions(t), names, running) {
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

func hasRunningState(functions []functionStatus, names []string, running bool) bool {
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

func getCallGraph(t *testing.T) callGraph {
	t.Helper()
	t.Log("[step] fetching callgraph")

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Get(defaultGatewayURL + "/system/callgraph")
	if err != nil {
		t.Fatalf("failed to get callgraph: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("callgraph request failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	var cg callGraph
	if err := json.Unmarshal(body, &cg); err != nil {
		t.Fatalf("failed to decode callgraph response: %v", err)
	}
	return cg
}

func edgeCount(cg callGraph, caller string, callee string) int {
	for _, e := range cg.Edges {
		if e.Caller == caller && e.Callee == callee {
			return e.Count
		}
	}
	return 0
}
