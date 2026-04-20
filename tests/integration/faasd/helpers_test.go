package faasd

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
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const (
	defaultGatewayURL = "http://127.0.0.1:8080"
	faasdVMName       = "faasd"
)

type gatewayAuth struct {
	user string
	pass string
}

type functionStatus struct {
	Name string `json:"name"`
}

type stackFile struct {
	Functions map[string]any `yaml:"functions"`
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)

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

func mustCommand(t *testing.T, timeout time.Duration, workdir string, stdin []byte, name string, args ...string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	if workdir != "" {
		cmd.Dir = workdir
	}
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "command failed: %s %s\noutput:\n%s", name, strings.Join(args, " "), string(out))
	return string(out)
}

func tryCommand(timeout time.Duration, workdir string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	if workdir != "" {
		cmd.Dir = workdir
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func gatewayURL() string {
	if v := strings.TrimSpace(os.Getenv("FAASD_TEST_GATEWAY_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return defaultGatewayURL
}

func requireFaasd(t *testing.T) (string, gatewayAuth) {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping integration tests in short mode")
	}

	if _, err := exec.LookPath("vagrant"); err != nil {
		t.Skip("vagrant not found in PATH")
	}
	if _, err := exec.LookPath("faas-cli"); err != nil {
		t.Skip("faas-cli not found in PATH")
	}

	status := mustCommand(t, 30*time.Second, repoRoot(t), nil, "vagrant", "status", faasdVMName, "--machine-readable")
	require.Contains(t, status, ","+faasdVMName+",state,running", "faasd VM is not running. Run: vagrant up faasd")

	baseURL := gatewayURL()
	waitForGateway(t, baseURL, 90*time.Second)
	loginFaasCLI(t, baseURL)
	auth := readGatewayAuth(t)

	return baseURL, auth
}

func waitForGateway(t *testing.T, baseURL string, timeout time.Duration) {
	t.Helper()

	client := &http.Client{Timeout: 3 * time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		url := strings.TrimRight(baseURL, "/") + "/system/functions"
		req, err := http.NewRequest(http.MethodGet, url, nil)
		require.NoError(t, err)
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	t.Fatalf("gateway at %s is not reachable within %s", baseURL, timeout)
}

func readGatewayAuth(t *testing.T) gatewayAuth {
	t.Helper()

	user := strings.TrimSpace(vagrantSSH(t, faasdVMName, "sudo cat /var/lib/faasd/secrets/basic-auth-user"))
	pass := strings.TrimSpace(vagrantSSH(t, faasdVMName, "sudo cat /var/lib/faasd/secrets/basic-auth-password"))
	require.NotEmpty(t, user)
	require.NotEmpty(t, pass)

	return gatewayAuth{user: user, pass: pass}
}

func loginFaasCLI(t *testing.T, baseURL string) {
	t.Helper()
	repo := repoRoot(t)
	cmd := fmt.Sprintf("vagrant ssh %s -c \"sudo cat /var/lib/faasd/secrets/basic-auth-password\" | faas-cli login --password-stdin --gateway %s", faasdVMName, baseURL)
	mustCommand(t, 60*time.Second, repo, nil, "sh", "-c", cmd)
}

func vagrantSSH(t *testing.T, vmName string, command string) string {
	t.Helper()
	return mustCommand(t, 2*time.Minute, repoRoot(t), nil, "vagrant", "ssh", vmName, "-c", command)
}

func requireLocalRegistryReachable(t *testing.T) {
	t.Helper()

	repo := repoRoot(t)
	if _, err := tryCommand(20*time.Second, repo, "docker", "ps"); err != nil {
		t.Skip("docker is not available on host; local build/push integration test requires docker")
	}

	statusHost, hostErr := tryCommand(20*time.Second, repo, "sh", "-c", "curl -s -o /dev/null -w '%{http_code}' http://registry.local:5050/v2/")
	if hostErr != nil {
		t.Skipf("registry.local:5050 is not reachable from host: %v", hostErr)
	}
	hostCode := strings.TrimSpace(statusHost)
	if hostCode != "200" && hostCode != "401" {
		t.Skipf("registry.local:5050 returned unexpected status from host: %q", hostCode)
	}
}

func parseStack(t *testing.T, stackPath string) stackFile {
	t.Helper()

	data, err := os.ReadFile(stackPath)
	require.NoError(t, err)

	var stack stackFile
	require.NoError(t, yaml.Unmarshal(data, &stack))
	require.NotEmpty(t, stack.Functions, "stack contains no functions: %s", stackPath)
	return stack
}

func stackFunctionNames(t *testing.T, stackPath string) []string {
	t.Helper()
	stack := parseStack(t, stackPath)
	names := make([]string, 0, len(stack.Functions))
	for name := range stack.Functions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func stackFunctionCount(t *testing.T, stackPath string) int {
	t.Helper()
	return len(parseStack(t, stackPath).Functions)
}

func removeWorkflowStack(t *testing.T, baseURL string, stackPath string) {
	t.Helper()
	_, _ = tryCommand(2*time.Minute, filepath.Dir(stackPath), "faas-cli", "remove", "--gateway", baseURL, "-f", stackPath)
}

func buildWorkflowStack(t *testing.T, stackPath string) {
	t.Helper()
	mustCommand(t, 20*time.Minute, filepath.Dir(stackPath), nil, "faas-cli", "build", "-f", stackPath)
}

func pushWorkflowStack(t *testing.T, stackPath string, parallelism int) {
	t.Helper()
	require.Greater(t, parallelism, 0)

	args := []string{"push", "--parallel", fmt.Sprintf("%d", parallelism), "-f", stackPath}
	var lastErr error
	var output string

	for attempt := 1; attempt <= 3; attempt++ {
		output, lastErr = tryCommand(30*time.Minute, filepath.Dir(stackPath), "faas-cli", args...)
		if lastErr == nil {
			return
		}

		t.Logf("faas-cli push attempt %d/3 failed for %s: %v", attempt, stackPath, lastErr)
		if strings.TrimSpace(output) != "" {
			t.Logf("faas-cli push output (attempt %d):\n%s", attempt, output)
		}

		if attempt < 3 {
			time.Sleep(2 * time.Second)
		}
	}

	t.Fatalf("faas-cli push failed after 3 attempts for %s: %v\noutput:\n%s", stackPath, lastErr, output)
}

func deployWorkflowStack(t *testing.T, baseURL string, stackPath string) {
	t.Helper()
	mustCommand(t, 10*time.Minute, filepath.Dir(stackPath), nil, "faas-cli", "deploy", "--read-template=false", "--gateway", baseURL, "-f", stackPath)
}

func listFunctions(t *testing.T, baseURL string, auth gatewayAuth) []functionStatus {
	t.Helper()

	url := strings.TrimRight(baseURL, "/") + "/system/functions"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)
	req.SetBasicAuth(auth.user, auth.pass)

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "list functions failed: %s", string(body))

	out := make([]functionStatus, 0)
	require.NoError(t, json.Unmarshal(body, &out), "invalid /system/functions payload: %s", string(body))
	return out
}

func waitForFunctionsPresent(t *testing.T, baseURL string, auth gatewayAuth, names []string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		functions := listFunctions(t, baseURL, auth)
		index := make(map[string]struct{}, len(functions))
		for _, fn := range functions {
			index[fn.Name] = struct{}{}
		}

		allPresent := true
		for _, name := range names {
			if _, ok := index[name]; !ok {
				allPresent = false
				break
			}
		}
		if allPresent {
			return
		}

		time.Sleep(700 * time.Millisecond)
	}

	t.Fatalf("functions %v not present within %s", names, timeout)
}

func warmFunctions(t *testing.T, baseURL string, auth gatewayAuth, names []string, timeout time.Duration) {
	t.Helper()

	for _, name := range names {
		warmFunctionEventually(t, baseURL, auth, name, timeout)
	}
}

func warmFunctionEventually(t *testing.T, baseURL string, auth gatewayAuth, functionName string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, _, err := invokeJSONOnce(t, baseURL, auth, functionName, map[string]any{"warmup": true})
		if err == nil && status > 0 {
			return
		}
		time.Sleep(700 * time.Millisecond)
	}

	t.Fatalf("warmup invoke for %s failed within %s", functionName, timeout)
}

func invokeJSONOnce(t *testing.T, baseURL string, auth gatewayAuth, functionName string, payload any) (int, []byte, error) {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal payload: %w", err)
	}

	url := strings.TrimRight(baseURL, "/") + "/function/" + functionName
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.SetBasicAuth(auth.user, auth.pass)
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, respBody, nil
}

func invokeJSONEventually(t *testing.T, baseURL string, auth gatewayAuth, functionName string, payload any, timeout time.Duration) []byte {
	t.Helper()

	deadline := time.Now().Add(timeout)
	var lastStatus int
	var lastBody []byte
	var lastErr error

	for time.Now().Before(deadline) {
		status, body, err := invokeJSONOnce(t, baseURL, auth, functionName, payload)
		if err == nil && status == http.StatusOK && len(body) > 0 {
			return body
		}
		lastStatus = status
		lastBody = body
		lastErr = err
		time.Sleep(700 * time.Millisecond)
	}

	if lastErr != nil {
		t.Fatalf("invoke %s failed within %s: %v", functionName, timeout, lastErr)
	}
	t.Fatalf("invoke %s failed within %s, last status=%d body=%s", functionName, timeout, lastStatus, string(lastBody))
	return nil
}

func decodeJSON[T any](t *testing.T, body []byte, out *T) {
	t.Helper()
	require.NoError(t, json.Unmarshal(body, out), "response is not valid JSON: %s", string(body))
}

func requireWorkflowEnvFile(t *testing.T, absPath string) {
	t.Helper()

	content, err := os.ReadFile(absPath)
	require.NoError(t, err, "required workflow env file missing or unreadable: %s", absPath)

	text := string(content)
	require.NotEmpty(t, strings.TrimSpace(text), "required workflow env file is empty: %s", absPath)
	require.Contains(t, text, "SUPABASE_URL", "required workflow env file must define SUPABASE_URL: %s", absPath)
	require.Contains(t, text, "SUPABASE_KEY", "required workflow env file must define SUPABASE_KEY: %s", absPath)
	require.NotContains(t, text, "your_supabase_project_url", "required workflow env file still contains placeholder values: %s", absPath)
	require.NotContains(t, text, "your_supabase_publishable_key", "required workflow env file still contains placeholder values: %s", absPath)
}

func eventually(t *testing.T, timeout time.Duration, interval time.Duration, fn func() bool) {
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
