package helpers

import (
	"bytes"
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

	"gopkg.in/yaml.v3"
)

const (
	DefaultFaasdGatewayURL = "http://127.0.0.1:8080"
	faasdVMName            = "faasd"
)

type FaasdGatewayAuth struct {
	User string
	Pass string
}

type faasdFunctionStatus struct {
	Name string `json:"name"`
}

type faasdStackFile struct {
	Functions map[string]any `yaml:"functions"`
}

func FaasdGatewayURL() string {
	if v := strings.TrimSpace(os.Getenv("FAASD_TEST_GATEWAY_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return DefaultFaasdGatewayURL
}

func RequireFaasd(t *testing.T) (string, FaasdGatewayAuth) {
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

	EnsureVagrantVMExclusive(t, faasdVMName, "tinyfaas")

	baseURL := FaasdGatewayURL()
	waitForFaasdGateway(t, baseURL, 90*time.Second)
	loginFaasCLI(t, baseURL)
	auth := readFaasdGatewayAuth(t)

	return baseURL, auth
}

func waitForFaasdGateway(t *testing.T, baseURL string, timeout time.Duration) {
	t.Helper()

	client := &http.Client{Timeout: 3 * time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		url := strings.TrimRight(baseURL, "/") + "/system/functions"
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			t.Fatalf("create request: %v", err)
		}
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

func readFaasdGatewayAuth(t *testing.T) FaasdGatewayAuth {
	t.Helper()

	user := strings.TrimSpace(vagrantSSH(t, faasdVMName, "sudo cat /var/lib/faasd/secrets/basic-auth-user"))
	pass := strings.TrimSpace(vagrantSSH(t, faasdVMName, "sudo cat /var/lib/faasd/secrets/basic-auth-password"))
	if user == "" {
		t.Fatal("faasd basic auth user is empty")
	}
	if pass == "" {
		t.Fatal("faasd basic auth password is empty")
	}

	return FaasdGatewayAuth{User: user, Pass: pass}
}

func loginFaasCLI(t *testing.T, baseURL string) {
	t.Helper()
	cmd := fmt.Sprintf("vagrant ssh %s -c \"sudo cat /var/lib/faasd/secrets/basic-auth-password\" | faas-cli login --password-stdin --gateway %s", faasdVMName, baseURL)
	MustRunCommand(t, CommandOptions{Timeout: 60 * time.Second, Dir: RepoRoot(t)}, "sh", "-c", cmd)
}

func vagrantSSH(t *testing.T, vmName string, command string) string {
	t.Helper()
	return MustRunCommand(t, CommandOptions{Timeout: 2 * time.Minute, Dir: RepoRoot(t)}, "vagrant", "ssh", vmName, "-c", command)
}

func RequireLocalRegistryReachable(t *testing.T) {
	t.Helper()

	repo := RepoRoot(t)
	if _, err := tryRunCommand(t, 20*time.Second, repo, "docker", "ps"); err != nil {
		t.Skip("docker is not available on host; local build/push integration test requires docker")
	}

	statusHost, hostErr := tryRunCommand(t, 20*time.Second, repo, "sh", "-c", "curl -s -o /dev/null -w '%{http_code}' http://registry.local:5050/v2/")
	if hostErr != nil {
		t.Skipf("registry.local:5050 is not reachable from host: %v", hostErr)
	}
	hostCode := strings.TrimSpace(statusHost)
	if hostCode != "200" && hostCode != "401" {
		t.Skipf("registry.local:5050 returned unexpected status from host: %q", hostCode)
	}
}

func parseFaasdStack(t *testing.T, stackPath string) faasdStackFile {
	t.Helper()

	data, err := os.ReadFile(stackPath)
	if err != nil {
		t.Fatalf("read stack file %s: %v", stackPath, err)
	}

	var stack faasdStackFile
	if err := yaml.Unmarshal(data, &stack); err != nil {
		t.Fatalf("decode stack file %s: %v", stackPath, err)
	}
	if len(stack.Functions) == 0 {
		t.Fatalf("stack contains no functions: %s", stackPath)
	}
	return stack
}

func FaasdStackFunctionNames(t *testing.T, stackPath string) []string {
	t.Helper()
	stack := parseFaasdStack(t, stackPath)
	names := make([]string, 0, len(stack.Functions))
	for name := range stack.Functions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func FaasdStackFunctionCount(t *testing.T, stackPath string) int {
	t.Helper()
	return len(parseFaasdStack(t, stackPath).Functions)
}

func RemoveFaasdWorkflowStack(t *testing.T, baseURL string, stackPath string) {
	t.Helper()
	_, _ = tryRunCommand(t, 2*time.Minute, filepath.Dir(stackPath), "faas-cli", "remove", "--gateway", baseURL, "-f", stackPath)
}

func BuildFaasdWorkflowStack(t *testing.T, stackPath string) {
	t.Helper()
	MustRunCommand(t, CommandOptions{Timeout: 20 * time.Minute, Dir: filepath.Dir(stackPath)}, "faas-cli", "build", "-f", stackPath)
}

func PushFaasdWorkflowStack(t *testing.T, stackPath string, parallelism int) {
	t.Helper()
	if parallelism <= 0 {
		t.Fatalf("parallelism must be > 0, got %d", parallelism)
	}

	args := []string{"push", "--parallel", fmt.Sprintf("%d", parallelism), "-f", stackPath}
	var lastErr error
	var output string

	for attempt := 1; attempt <= 3; attempt++ {
		output, lastErr = tryRunCommand(t, 30*time.Minute, filepath.Dir(stackPath), "faas-cli", args...)
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

func DeployFaasdWorkflowStack(t *testing.T, baseURL string, stackPath string) {
	t.Helper()
	MustRunCommand(t, CommandOptions{Timeout: 10 * time.Minute, Dir: filepath.Dir(stackPath)}, "faas-cli", "deploy", "--read-template=false", "--gateway", baseURL, "-f", stackPath)
}

func WaitForFaasdFunctionsPresent(t *testing.T, baseURL string, auth FaasdGatewayAuth, names []string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		functions := listFaasdFunctions(t, baseURL, auth)
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

func WarmFaasdFunctions(t *testing.T, baseURL string, auth FaasdGatewayAuth, names []string, timeout time.Duration) {
	t.Helper()

	for _, name := range names {
		warmFaasdFunctionEventually(t, baseURL, auth, name, timeout)
	}
}

func InvokeFaasdJSONOnce(t *testing.T, baseURL string, auth FaasdGatewayAuth, functionName string, payload any) (int, []byte, error) {
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
	req.SetBasicAuth(auth.User, auth.Pass)
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, respBody, nil
}

func InvokeFaasdJSONEventually(t *testing.T, baseURL string, auth FaasdGatewayAuth, functionName string, payload any, timeout time.Duration) []byte {
	t.Helper()

	deadline := time.Now().Add(timeout)
	var lastStatus int
	var lastBody []byte
	var lastErr error

	for time.Now().Before(deadline) {
		status, body, err := InvokeFaasdJSONOnce(t, baseURL, auth, functionName, payload)
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

func tryRunCommand(t *testing.T, timeout time.Duration, workdir string, name string, args ...string) (string, error) {
	t.Helper()
	return TryRunCommand(t, CommandOptions{Timeout: timeout, Dir: workdir}, name, args...)
}

func listFaasdFunctions(t *testing.T, baseURL string, auth FaasdGatewayAuth) []faasdFunctionStatus {
	t.Helper()

	url := strings.TrimRight(baseURL, "/") + "/system/functions"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("create list request: %v", err)
	}
	req.SetBasicAuth(auth.User, auth.Pass)

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("list functions request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read list functions response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list functions failed: %s", string(body))
	}

	out := make([]faasdFunctionStatus, 0)
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("invalid /system/functions payload: %s err=%v", string(body), err)
	}
	return out
}

func warmFaasdFunctionEventually(t *testing.T, baseURL string, auth FaasdGatewayAuth, functionName string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, _, err := InvokeFaasdJSONOnce(t, baseURL, auth, functionName, map[string]any{"warmup": true})
		if err == nil && status > 0 {
			return
		}
		time.Sleep(700 * time.Millisecond)
	}

	t.Fatalf("warmup invoke for %s failed within %s", functionName, timeout)
}
