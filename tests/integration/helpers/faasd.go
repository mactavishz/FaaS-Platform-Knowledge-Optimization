package helpers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	sdkstack "github.com/openfaas/go-sdk/stack"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	DEFAULT_FAASD_GATEWAY_URL = "http://127.0.0.1:8080"
	FAASD_VM_NAME             = "faasd"
	registryTypeEnvVar        = "REGISTRY_TYPE"
	registryTypeLocal         = "local"
	registryTypeRemote        = "remote"
)

type FaasdGatewayAuth struct {
	User     string
	Pass     string
	Password string
}

type FunctionResources struct {
	Memory string `json:"memory" yaml:"memory"`
	CPU    string `json:"cpu" yaml:"cpu"`
}

type FaasdFunctionStatus struct {
	Name              string             `json:"name"`
	Image             string             `json:"image"`
	Replicas          uint64             `json:"replicas,omitempty"`
	AvailableReplicas uint64             `json:"availableReplicas,omitempty"`
	Limits            *FunctionResources `json:"limits,omitempty"`
}

type DeployOptions struct {
	Image       string
	CPULimit    string
	MemoryLimit string
}

type ContainerInfo struct {
	ID   string `json:"ID"`
	Spec struct {
		Linux struct {
			Resources struct {
				Memory struct {
					Limit *int64 `json:"limit"`
				} `json:"memory"`
				CPU struct {
					Quota  *int64  `json:"quota"`
					Period *uint64 `json:"period"`
				} `json:"cpu"`
			} `json:"resources"`
		} `json:"linux"`
	} `json:"Spec"`
}

func FaasdGatewayURL() string {
	if v := strings.TrimSpace(os.Getenv("FAASD_TEST_GATEWAY_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return DEFAULT_FAASD_GATEWAY_URL
}

func authSecret(auth FaasdGatewayAuth) string {
	if strings.TrimSpace(auth.Pass) != "" {
		return auth.Pass
	}
	return auth.Password
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

	EnsureVagrantVMExclusive(t, FAASD_VM_NAME, "tinyfaas")

	baseURL := FaasdGatewayURL()
	waitForFaasdGateway(t, baseURL, 90*time.Second)
	loginFaasCLI(t, baseURL)
	auth := readFaasdGatewayAuth(t)

	return baseURL, auth
}

func RebuildFaasd(t *testing.T, envProfile string) {
	t.Helper()
	t.Logf("[step] rebuilding faasd with profile=%s", envProfile)

	cmd := fmt.Sprintf("PROJECT_ROOT=/vagrant ENV_FILE=/vagrant/tests/integration/env/%s bash /vagrant/scripts/build-faasd.sh", envProfile)
	MustRunCommand(t, CommandOptions{Timeout: 35 * time.Minute, Dir: RepoRoot(t)}, "vagrant", "ssh", FAASD_VM_NAME, "-c", cmd)
	t.Log("[step] faasd rebuild complete")
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

	user := strings.TrimSpace(vagrantSSH(t, FAASD_VM_NAME, "sudo cat /var/lib/faasd/secrets/basic-auth-user"))
	pass := strings.TrimSpace(vagrantSSH(t, FAASD_VM_NAME, "sudo cat /var/lib/faasd/secrets/basic-auth-password"))
	if user == "" {
		t.Fatal("faasd basic auth user is empty")
	}
	if pass == "" {
		t.Fatal("faasd basic auth password is empty")
	}

	return FaasdGatewayAuth{User: user, Pass: pass, Password: pass}
}

func loginFaasCLI(t *testing.T, baseURL string) {
	t.Helper()
	cmd := fmt.Sprintf("vagrant ssh %s -c \"sudo cat /var/lib/faasd/secrets/basic-auth-password\" | faas-cli login --password-stdin --gateway %s", FAASD_VM_NAME, baseURL)
	MustRunCommand(t, CommandOptions{Timeout: 60 * time.Second, Dir: RepoRoot(t)}, "sh", "-c", cmd)
}

func vagrantSSH(t *testing.T, vmName string, command string) string {
	t.Helper()
	return VagrantSSH(t, vmName, command)
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

func registryType(t *testing.T) string {
	t.Helper()

	v := strings.ToLower(strings.TrimSpace(os.Getenv(registryTypeEnvVar)))
	if v == "" {
		return registryTypeRemote
	}

	switch v {
	case registryTypeLocal, registryTypeRemote:
		return v
	default:
		t.Fatalf("invalid %s=%q, expected %q or %q", registryTypeEnvVar, v, registryTypeLocal, registryTypeRemote)
		return ""
	}
}

func parseFaasdStack(t *testing.T, stackPath string, envsubst bool) *sdkstack.Services {
	t.Helper()

	stack, err := sdkstack.ParseYAMLFile(stackPath, "", "", envsubst)
	if err != nil {
		t.Fatalf("parse stack file %s: %v", stackPath, err)
	}
	if stack == nil || len(stack.Functions) == 0 {
		t.Fatalf("stack contains no functions: %s", stackPath)
	}
	return stack
}

func FaasdStackFunctionNames(t *testing.T, stackPath string) []string {
	t.Helper()
	stack := parseFaasdStack(t, stackPath, true)
	names := make([]string, 0, len(stack.Functions))
	for name := range stack.Functions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func FaasdStackFunctionCount(t *testing.T, stackPath string) int {
	t.Helper()
	return len(parseFaasdStack(t, stackPath, true).Functions)
}

func ParseFixtureStack(t *testing.T, fixtureDir string) *sdkstack.Services {
	t.Helper()
	stackPath := filepath.Join(fixtureDir, "stack.yaml")
	return parseFaasdStack(t, stackPath, false)
}

func FixtureFunction(t *testing.T, fixtureDir string, sourceName string) sdkstack.Function {
	t.Helper()
	services := ParseFixtureStack(t, fixtureDir)
	fn, ok := services.Functions[sourceName]
	if !ok {
		t.Fatalf("source function %q not found in stack", sourceName)
	}
	fn.Name = sourceName
	return fn
}

func RemoveFaasdWorkflowStack(t *testing.T, baseURL string, stackPath string) {
	t.Helper()
	_, _ = TryRunCommand(t, CommandOptions{Timeout: 2 * time.Minute, Dir: filepath.Dir(stackPath)}, "faas-cli", "remove", "--gateway", baseURL, "-f", stackPath)
}

func BuildFaasdWorkflowStack(t *testing.T, stackPath string) {
	t.Helper()
	MustRunCommand(t, CommandOptions{Timeout: 20 * time.Minute, Dir: filepath.Dir(stackPath)}, "faas-cli", "build", "-f", stackPath)
}

func PrepareFaasdWorkflowStack(t *testing.T, stackPath string) {
	t.Helper()

	if registryType(t) != registryTypeLocal {
		prefix := os.Getenv("REGISTRY_PREFIX")
		require.NotEmpty(t, prefix, "REGISTRY_PREFIX must be set for remote registry")
		t.Logf("Using remote registry, remote registry prefix=%s", prefix)
		return
	}

	t.Log("Using local registry, ensuring local registry is reachable")
	RequireLocalRegistryReachable(t)
	BuildFaasdWorkflowStack(t, stackPath)

	parallelism := FaasdStackFunctionCount(t, stackPath)
	if parallelism <= 0 {
		parallelism = 1
	}
	PushFaasdWorkflowStack(t, stackPath, parallelism)
}

func pushImageWithRetries(t *testing.T, workdir string, image string) error {
	t.Helper()

	var lastErr error
	var output string
	for attempt := 1; attempt <= 3; attempt++ {
		output, lastErr = tryRunCommand(t, 30*time.Minute, workdir, "docker", "push", "-q", image)
		if lastErr == nil {
			return nil
		}

		t.Logf("docker push attempt %d/3 failed for %s: %v", attempt, image, lastErr)
		if strings.TrimSpace(output) != "" {
			t.Logf("docker push output (attempt %d):\n%s", attempt, output)
		}

		if attempt < 3 {
			time.Sleep(2 * time.Second)
		}
	}

	return fmt.Errorf("docker push failed after 3 attempts for %s: %w\noutput:\n%s", image, lastErr, output)
}

func PushFaasdWorkflowStack(t *testing.T, stackPath string, parallelism int) {
	t.Helper()
	if parallelism <= 0 {
		t.Fatalf("parallelism must be > 0, got %d", parallelism)
	}

	services := parseFaasdStack(t, stackPath, true)
	imagesByName := make(map[string]struct{})
	for _, fn := range services.Functions {
		if fn.SkipBuild {
			continue
		}
		img := strings.TrimSpace(fn.Image)
		if img == "" {
			continue
		}
		imagesByName[img] = struct{}{}
	}

	images := make([]string, 0, len(imagesByName))
	for img := range imagesByName {
		images = append(images, img)
	}
	sort.Strings(images)

	if len(images) == 0 {
		t.Fatalf("no pushable function images found in stack: %s", stackPath)
	}

	workdir := filepath.Dir(stackPath)
	jobs := make(chan string)
	errCh := make(chan error, len(images))

	workers := parallelism
	if workers > len(images) {
		workers = len(images)
	}

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for image := range jobs {
				if err := pushImageWithRetries(t, workdir, image); err != nil {
					errCh <- err
				}
			}
		}()
	}

	for _, image := range images {
		jobs <- image
	}
	close(jobs)
	wg.Wait()
	close(errCh)

	if len(errCh) > 0 {
		var errs []error
		for err := range errCh {
			errs = append(errs, err)
		}
		t.Fatal(errors.Join(errs...))
	}
}

func BuildStack(t *testing.T, stackPath string) {
	t.Helper()
	BuildFaasdWorkflowStack(t, stackPath)
}

func PushStack(t *testing.T, stackPath string) {
	t.Helper()
	parallelism := FaasdStackFunctionCount(t, stackPath)
	if parallelism <= 0 {
		parallelism = 1
	}
	PushFaasdWorkflowStack(t, stackPath, parallelism)
}

func DeployStack(t *testing.T, stackPath string, gateway string) {
	t.Helper()
	DeployFaasdWorkflowStack(t, gateway, stackPath)
}

func DeployFaasdWorkflowStack(t *testing.T, baseURL string, stackPath string) {
	DeployFaasdWorkflowStackWithEnvs(t, baseURL, stackPath, nil)
}

func DeployFaasdWorkflowStackWithEnvs(t *testing.T, baseURL string, stackPath string, envs map[string]string) {
	t.Helper()
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		args := []string{"deploy", "--read-template=false", "--gateway", baseURL, "-f", stackPath}
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

		_, err := TryRunCommand(t, CommandOptions{Timeout: 10 * time.Minute, Dir: filepath.Dir(stackPath)}, "faas-cli", args...)
		if err == nil {
			return
		}

		lastErr = err
		t.Logf("faas-cli deploy attempt %d/3 failed for stack=%s: %v", attempt, stackPath, err)
		RemoveFaasdWorkflowStack(t, baseURL, stackPath)
		time.Sleep(2 * time.Second)
	}

	t.Fatalf("failed to deploy stack after retries stack=%s err=%v", stackPath, lastErr)
}

func GetFaasdCallGraph(t *testing.T, baseURL string, auth FaasdGatewayAuth) CallGraph {
	t.Helper()

	endpoint := strings.TrimRight(baseURL, "/") + "/system/callgraph"
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatalf("create callgraph request: %v", err)
	}
	req.SetBasicAuth(auth.User, authSecret(auth))

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("callgraph request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read callgraph response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("callgraph request failed status=%d body=%s", resp.StatusCode, string(body))
	}

	var cg CallGraph
	if err := json.Unmarshal(body, &cg); err != nil {
		t.Fatalf("decode callgraph response: %v", err)
	}

	return cg
}

func GetFaasdFunctionCallGraphStats(t *testing.T, baseURL string, auth FaasdGatewayAuth, functionName string) FunctionCallGraphStats {
	t.Helper()

	endpoint := strings.TrimRight(baseURL, "/") + "/system/callgraph/function/" + functionName
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatalf("create callgraph function request: %v", err)
	}
	req.SetBasicAuth(auth.User, authSecret(auth))

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("callgraph function request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read callgraph function response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("callgraph function request failed status=%d body=%s", resp.StatusCode, string(body))
	}

	var stats FunctionCallGraphStats
	if err := json.Unmarshal(body, &stats); err != nil {
		t.Fatalf("decode callgraph function response: %v", err)
	}

	return stats
}

func UniqueFunctionName(prefix string) string {
	p := strings.ToLower(strings.TrimSpace(prefix))
	p = strings.ReplaceAll(p, "_", "-")
	p = strings.ReplaceAll(p, " ", "-")
	if p == "" {
		p = "fn"
	}
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	name := p + "-" + suffix[len(suffix)-8:]
	if len(name) > 63 {
		name = name[:63]
		name = strings.TrimRight(name, "-")
	}
	return name
}

func RemoveFunction(t *testing.T, functionName string, gateway string) {
	t.Helper()
	_, _ = TryRunCommand(t, CommandOptions{Timeout: 60 * time.Second}, "faas-cli", "remove", functionName, "--gateway", gateway)
}

func DeployFunction(t *testing.T, gateway string, functionName string, fn sdkstack.Function, opts DeployOptions) {
	t.Helper()

	image := strings.TrimSpace(opts.Image)
	if image == "" {
		image = strings.TrimSpace(fn.Image)
	}
	if image == "" {
		t.Fatal("function image is required for deploy")
	}

	cpu := strings.TrimSpace(opts.CPULimit)
	memory := strings.TrimSpace(opts.MemoryLimit)

	if fn.Limits != nil {
		if cpu == "" {
			cpu = strings.TrimSpace(fn.Limits.CPU)
		}
		if memory == "" {
			memory = strings.TrimSpace(fn.Limits.Memory)
		}
	}

	args := []string{
		"deploy",
		"--gateway", gateway,
		"--name", functionName,
		"--image", image,
	}

	if cpu != "" {
		args = append(args, "--cpu-limit", cpu)
	}
	if memory != "" {
		args = append(args, "--memory-limit", memory)
	}

	MustRunCommand(t, CommandOptions{Timeout: 10 * time.Minute, Dir: RepoRoot(t)}, "faas-cli", args...)
}

func InvokeFaasdFunction(t *testing.T, baseURL string, auth FaasdGatewayAuth, functionName string, payload io.Reader) (int, []byte) {
	t.Helper()

	url := strings.TrimRight(baseURL, "/") + "/function/" + functionName
	req, err := http.NewRequest(http.MethodPost, url, payload)
	if err != nil {
		t.Fatalf("failed to create invoke request: %v", err)
	}
	req.SetBasicAuth(auth.User, authSecret(auth))
	req.Header.Set("Content-Type", "text/plain")

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("invoke request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read invoke response: %v", err)
	}

	return resp.StatusCode, body
}

func InvokeFaasdFunctionEventually(t *testing.T, baseURL string, auth FaasdGatewayAuth, functionName string, payload []byte, expectedStatus int, timeout time.Duration) (int, []byte) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	var lastStatus int
	var lastBody []byte

	for time.Now().Before(deadline) {
		status, body := InvokeFaasdFunction(t, baseURL, auth, functionName, bytes.NewReader(payload))
		lastStatus = status
		lastBody = body
		if status == expectedStatus {
			return status, body
		}
		time.Sleep(700 * time.Millisecond)
	}

	t.Fatalf("invoke %q did not reach status %d within %s, last status=%d, body=%s", functionName, expectedStatus, timeout, lastStatus, string(lastBody))
	return 0, nil
}

func WaitForFaasdFunctionsPresent(t *testing.T, baseURL string, auth FaasdGatewayAuth, names []string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		functions := ListFaasdFunctions(t, baseURL, auth)
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
	req.SetBasicAuth(auth.User, authSecret(auth))
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, respBody, nil
}

func InvokeFaasdJSON(t *testing.T, baseURL string, auth FaasdGatewayAuth, functionName string, payload any) []byte {
	t.Helper()

	status, body, err := InvokeFaasdJSONOnce(t, baseURL, auth, functionName, payload)
	if err != nil {
		t.Fatalf("invoke %s failed: %v", functionName, err)
	}

	if status != http.StatusOK {
		t.Fatalf("invoke %s failed, expected status=%d got status=%d body=%s", functionName, http.StatusOK, status, string(body))
	}

	return body
}

func InvokeFaasdAsyncJSON(t *testing.T, baseURL string, auth FaasdGatewayAuth, functionName string, payload any, callbackURL string) (int, []byte) {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	invokeURL := strings.TrimRight(baseURL, "/") + "/async-function/" + functionName
	req, err := http.NewRequest(http.MethodPost, invokeURL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create async invoke request: %v", err)
	}
	req.SetBasicAuth(auth.User, authSecret(auth))
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(callbackURL) != "" {
		req.Header.Set("X-Callback-Url", callbackURL)
	}

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("async invoke request failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, respBody
}

func FaasdServiceLogs(t *testing.T, serviceName string, since string) string {
	t.Helper()

	query := fmt.Sprintf("sudo journalctl -o cat -t openfaas:%s", serviceName)
	if strings.TrimSpace(since) != "" {
		query = query + fmt.Sprintf(" --since '%s'", since)
	}

	return vagrantSSH(t, FAASD_VM_NAME, query)
}

func WaitForFaasdServiceLogMatch(t *testing.T, serviceName string, needle string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	since := "10 minutes ago"

	for time.Now().Before(deadline) {
		logs := FaasdServiceLogs(t, serviceName, since)
		if strings.Contains(logs, needle) {
			return
		}
		time.Sleep(1 * time.Second)
	}

	t.Fatalf("service logs for %s did not contain %q within %s", serviceName, needle, timeout)
}

func FaasdFunctionJournalLogs(t *testing.T, functionName string, since string) string {
	t.Helper()

	query := fmt.Sprintf("sudo journalctl -o cat -t openfaas-fn:%s", functionName)
	if strings.TrimSpace(since) != "" {
		query = query + fmt.Sprintf(" --since '%s'", since)
	}

	return vagrantSSH(t, FAASD_VM_NAME, query)
}

func WaitForFaasdFunctionJournalLogMatch(t *testing.T, functionName string, needle string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	since := "10 minutes ago"

	for time.Now().Before(deadline) {
		logs := FaasdFunctionJournalLogs(t, functionName, since)
		if strings.Contains(logs, needle) {
			return
		}
		time.Sleep(1 * time.Second)
	}

	t.Fatalf("function logs for %s did not contain %q within %s", functionName, needle, timeout)
}

func FaasdFunctionLogs(t *testing.T, baseURL string, auth FaasdGatewayAuth, functionName string) string {
	t.Helper()

	endpoint := strings.TrimRight(baseURL, "/") + "/system/logs?name=" + url.QueryEscape(functionName)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatalf("create logs request: %v", err)
	}
	req.SetBasicAuth(auth.User, authSecret(auth))

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("logs request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read logs response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logs request for %s failed status=%d body=%s", functionName, resp.StatusCode, string(body))
	}

	return string(body)
}

func tryRunCommand(t *testing.T, timeout time.Duration, workdir string, name string, args ...string) (string, error) {
	t.Helper()
	return TryRunCommand(t, CommandOptions{Timeout: timeout, Dir: workdir}, name, args...)
}

func ListFaasdFunctions(t *testing.T, baseURL string, auth FaasdGatewayAuth) []FaasdFunctionStatus {
	t.Helper()

	url := strings.TrimRight(baseURL, "/") + "/system/functions"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("create list request: %v", err)
	}
	req.SetBasicAuth(auth.User, authSecret(auth))

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

	out := make([]FaasdFunctionStatus, 0)
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("invalid /system/functions payload: %s err=%v", string(body), err)
	}
	return out
}

func GetFaasdFunction(t *testing.T, baseURL string, auth FaasdGatewayAuth, functionName string) FaasdFunctionStatus {
	t.Helper()
	for _, fn := range ListFaasdFunctions(t, baseURL, auth) {
		if fn.Name == functionName {
			return fn
		}
	}
	t.Fatalf("function %q not found in /system/functions", functionName)
	return FaasdFunctionStatus{}
}

func WaitForFaasdFunction(t *testing.T, baseURL string, auth FaasdGatewayAuth, functionName string, timeout time.Duration) FaasdFunctionStatus {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, fn := range ListFaasdFunctions(t, baseURL, auth) {
			if fn.Name == functionName {
				return fn
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	t.Fatalf("function %q not found in /system/functions within %s", functionName, timeout)
	return FaasdFunctionStatus{}
}

func WaitForContainerInfo(t *testing.T, functionName string, timeout time.Duration) ContainerInfo {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out := vagrantSSH(t, FAASD_VM_NAME, "sudo ctr -n openfaas-fn c info "+functionName+" 2>/dev/null || true")
		trimmed := strings.TrimSpace(out)
		if trimmed == "" {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		var info ContainerInfo
		if err := json.Unmarshal([]byte(trimmed), &info); err == nil && info.ID == functionName {
			return info
		}

		time.Sleep(500 * time.Millisecond)
	}

	t.Fatalf("container info for %q not found in openfaas-fn namespace within %s", functionName, timeout)
	return ContainerInfo{}
}

func ExistsFaasdContainer(t *testing.T, functionName string) bool {
	out := vagrantSSH(t, FAASD_VM_NAME, fmt.Sprintf("sudo ctr -n openfaas-fn c list -q 'id==%s'", functionName))
	out = strings.TrimSpace(out)
	if out == functionName {
		return true
	}
	return false
}

func MemoryBytes(t *testing.T, memory string) int64 {
	t.Helper()
	q, err := resource.ParseQuantity(memory)
	if err != nil {
		t.Fatalf("invalid memory quantity %q: %v", memory, err)
	}
	return q.Value()
}

func CPUNano(t *testing.T, cpu string) int64 {
	t.Helper()
	q, err := resource.ParseQuantity(cpu)
	if err != nil {
		t.Fatalf("invalid cpu quantity %q: %v", cpu, err)
	}
	return q.MilliValue() * 1_000_000
}

func CPUQuotaFromNano(nano int64, period uint64) int64 {
	periodInt64 := int64(period)
	quota := (nano/1_000_000_000)*periodInt64 + ((nano%1_000_000_000)*periodInt64)/1_000_000_000
	if quota < 1 {
		return 1
	}
	return quota
}
