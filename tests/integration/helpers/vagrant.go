package helpers

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func IsVagrantVMRunning(t *testing.T, vmName string) bool {
	t.Helper()
	status := MustRunCommand(t, CommandOptions{Timeout: 30 * time.Second, Dir: RepoRoot(t)}, "vagrant", "status", vmName, "--machine-readable")
	return strings.Contains(status, ","+vmName+",state,running")
}

func SuspendVagrantVMIfRunning(t *testing.T, vmName string) {
	t.Helper()
	if !IsVagrantVMRunning(t, vmName) {
		return
	}
	t.Logf("[step] suspending %s VM", vmName)
	MustRunCommand(t, CommandOptions{Timeout: 5 * time.Minute, Dir: RepoRoot(t)}, "vagrant", "suspend", vmName)
}

func EnsureVagrantVMExclusive(t *testing.T, targetVM string, otherVM string) {
	t.Helper()

	if _, err := exec.LookPath("vagrant"); err != nil {
		t.Skip("vagrant not found in PATH")
	}

	if IsVagrantVMRunning(t, otherVM) {
		t.Logf("[step] suspending %s VM before %s tests", otherVM, targetVM)
		MustRunCommand(t, CommandOptions{Timeout: 5 * time.Minute, Dir: RepoRoot(t)}, "vagrant", "suspend", otherVM)
	}

	t.Logf("[step] ensuring %s VM is running", targetVM)
	if !IsVagrantVMRunning(t, targetVM) {
		MustRunCommand(t, CommandOptions{Timeout: 10 * time.Minute, Dir: RepoRoot(t)}, "vagrant", "up", targetVM)
	}

	if !IsVagrantVMRunning(t, targetVM) {
		t.Fatalf("%s VM is not running after ensure step. Run: vagrant up %s", targetVM, targetVM)
	}

	if IsVagrantVMRunning(t, otherVM) {
		t.Fatalf("expected %s VM to be suspended while running %s tests", otherVM, targetVM)
	}

	t.Logf("[step] vagrant VM exclusivity ensured target=%s other=%s", targetVM, otherVM)
}

func VagrantSSHCommand(vmName string, command string) string {
	return fmt.Sprintf("vagrant ssh %s -c %q", vmName, command)
}
