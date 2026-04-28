package helpers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type CommandOptions struct {
	Timeout time.Duration
	Dir     string
	Stdin   []byte
	Env     map[string]string
}

func debugCommandOutputEnabled() bool {
	return strings.TrimSpace(os.Getenv("INTEGRATION_DEBUG")) == "1"
}

func runCommand(t *testing.T, opts CommandOptions, name string, args ...string) (string, string, error) {
	t.Helper()
	t.Logf("[cmd:start] %s %s", name, strings.Join(args, " "))
	start := time.Now()

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}
	if len(opts.Env) > 0 {
		env := os.Environ()
		for k, v := range opts.Env {
			env = append(env, k+"="+v)
		}
		cmd.Env = env
	}
	if opts.Stdin != nil {
		cmd.Stdin = bytes.NewReader(opts.Stdin)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if debugCommandOutputEnabled() {
		cmd.Stdout = io.MultiWriter(os.Stdout, &stdout)
		cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	} else {
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
	}

	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return stdout.String(), stderr.String(), fmt.Errorf("command timed out after %s: %s %s", timeout, name, strings.Join(args, " "))
		}
		return stdout.String(), stderr.String(), fmt.Errorf("command failed: %s %s: %w", name, strings.Join(args, " "), err)
	}

	t.Logf("[cmd:done] %s %s (%s)", name, strings.Join(args, " "), time.Since(start).Round(time.Millisecond))
	return stdout.String(), stderr.String(), nil
}

func formatCommandFailure(err error, stdout string, stderr string) error {
	if strings.TrimSpace(stdout) == "" && strings.TrimSpace(stderr) == "" {
		return err
	}

	var b strings.Builder
	b.WriteString(err.Error())
	if strings.TrimSpace(stdout) != "" {
		b.WriteString("\n[stdout]\n")
		b.WriteString(stdout)
	}
	if strings.TrimSpace(stderr) != "" {
		b.WriteString("\n[stderr]\n")
		b.WriteString(stderr)
	}
	return fmt.Errorf("%s", b.String())
}

func MustRunCommand(t *testing.T, opts CommandOptions, name string, args ...string) string {
	t.Helper()
	stdout, stderr, err := runCommand(t, opts, name, args...)
	if err != nil {
		t.Fatal(formatCommandFailure(err, stdout, stderr))
	}
	return stdout
}

func TryRunCommand(t *testing.T, opts CommandOptions, name string, args ...string) (string, error) {
	t.Helper()
	stdout, stderr, err := runCommand(t, opts, name, args...)
	if err != nil {
		return stdout, formatCommandFailure(err, stdout, stderr)
	}
	return stdout, nil
}
