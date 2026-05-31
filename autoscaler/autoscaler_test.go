package autoscaler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func nopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type mockScaleOperation struct {
	mu            sync.Mutex
	downCalls     int
	upCalls       int
	downErr       error
	upErr         error
	downDelay     time.Duration
	upDelay       time.Duration
	downCh        chan struct{}
	downReleaseCh chan struct{}
}

func (m *mockScaleOperation) ScaleDown(functionName string) error {
	m.mu.Lock()
	m.downCalls++
	delay := m.downDelay
	err := m.downErr
	downCh := m.downCh
	release := m.downReleaseCh
	m.mu.Unlock()

	if downCh != nil {
		close(downCh)
	}
	if release != nil {
		<-release
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	return err
}

func (m *mockScaleOperation) ScaleUp(functionName string) error {
	m.mu.Lock()
	m.upCalls++
	delay := m.upDelay
	err := m.upErr
	m.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	return err
}

func (m *mockScaleOperation) DownCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.downCalls
}

func (m *mockScaleOperation) UpCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.upCalls
}

func markFunctionIdle(t *testing.T, as *AutoScaler, name string) {
	t.Helper()

	entry, ok := as.getFunctionEntry(name)
	if !ok {
		t.Fatalf("expected function %s to be registered", name)
	}

	entry.mu.Lock()
	entry.state.LastAccessTime = time.Now().Add(-entry.state.IdleDuration - time.Second)
	entry.state.LastActiveTime = entry.state.LastAccessTime
	entry.mu.Unlock()
}

func TestRegisterFunctionInitialState(t *testing.T) {
	as := New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: time.Minute}, &mockScaleOperation{}, nopLogger())
	as.RegisterFunctionWithState("fn", nil, StateScaledDown)

	state, ok := as.GetState("fn")
	assert.True(t, ok)
	assert.Equal(t, StateScaledDown, state)
}

func TestBlockedTransitions(t *testing.T) {
	as := New(Config{Enabled: true, Platform: "tinyfaas", DefaultIdleDuration: time.Minute}, &mockScaleOperation{}, nopLogger())
	as.RegisterFunction("fn", nil)

	err := as.StartInvocation("fn")
	assert.NoError(t, err)
	state, _ := as.GetState("fn")
	assert.Equal(t, StateBlocked, state)

	as.EndInvocation("fn")
	state, _ = as.GetState("fn")
	assert.Equal(t, StateActive, state)
}

func TestScaleDownWhenIdleWaitsForBlockedInvocation(t *testing.T) {
	m := &mockScaleOperation{}
	as := New(Config{Enabled: true, Platform: "tinyfaas", DefaultIdleDuration: time.Minute}, m, nopLogger())
	as.RegisterFunction("fn", nil)
	markFunctionIdle(t, as, "fn")

	assert.NoError(t, as.StartInvocation("fn"))

	done := make(chan error, 1)
	go func() {
		done <- as.ScaleDownWhenIdle(context.TODO(), "fn")
	}()

	time.Sleep(80 * time.Millisecond)
	assert.Equal(t, 0, m.DownCalls())

	as.EndInvocation("fn")
	assert.NoError(t, <-done)
	assert.Equal(t, 0, m.DownCalls())
	state, _ := as.GetState("fn")
	assert.Equal(t, StateActive, state)
}

func TestEnsureActiveFromScaledDown(t *testing.T) {
	m := &mockScaleOperation{}
	as := New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: time.Minute}, m, nopLogger())
	as.RegisterFunctionWithState("fn", nil, StateScaledDown)

	err := as.ScaleUpWhenReady(context.TODO(), "fn")
	assert.NoError(t, err)
	assert.Equal(t, 1, m.UpCalls())
	state, _ := as.GetState("fn")
	assert.Equal(t, StateActive, state)
}

func TestEnsureActiveConcurrentCollapsesScaleUp(t *testing.T) {
	m := &mockScaleOperation{upDelay: 80 * time.Millisecond}
	as := New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: time.Minute}, m, nopLogger())
	as.RegisterFunctionWithState("fn", nil, StateScaledDown)

	err1 := make(chan error, 1)
	err2 := make(chan error, 1)
	go func() { err1 <- as.ScaleUpWhenReady(context.TODO(), "fn") }()
	go func() { err2 <- as.ScaleUpWhenReady(context.TODO(), "fn") }()

	assert.NoError(t, <-err1)
	assert.NoError(t, <-err2)
	assert.Equal(t, 1, m.UpCalls())
}

func TestScaleDownFailureReturnsActive(t *testing.T) {
	m := &mockScaleOperation{downErr: errors.New("boom")}
	as := New(Config{Enabled: true, Platform: "tinyfaas", DefaultIdleDuration: time.Minute}, m, nopLogger())
	as.RegisterFunction("fn", nil)
	markFunctionIdle(t, as, "fn")

	err := as.ScaleDownWhenIdle(context.TODO(), "fn")
	assert.Error(t, err)
	state, _ := as.GetState("fn")
	assert.Equal(t, StateActive, state)
}

func TestScaleUpFailureReturnsScaledDown(t *testing.T) {
	m := &mockScaleOperation{upErr: errors.New("boom")}
	as := New(Config{Enabled: true, Platform: "tinyfaas", DefaultIdleDuration: time.Minute}, m, nopLogger())
	as.RegisterFunctionWithState("fn", nil, StateScaledDown)

	err := as.ScaleUpWhenReady(context.TODO(), "fn")
	assert.Error(t, err)
	state, _ := as.GetState("fn")
	assert.Equal(t, StateScaledDown, state)
}

func TestScaleUpWhenReadyBlockedReturnsImmediately(t *testing.T) {
	m := &mockScaleOperation{}
	as := New(Config{Enabled: true, Platform: "tinyfaas", DefaultIdleDuration: time.Minute}, m, nopLogger())
	as.RegisterFunction("fn", nil)

	assert.NoError(t, as.StartInvocation("fn"))
	err := as.ScaleUpWhenReady(context.TODO(), "fn")
	assert.NoError(t, err)
	assert.Equal(t, 0, m.UpCalls())

	as.EndInvocation("fn")
}

func TestScaleUpWhenReadyRefreshesActiveFunctionActivity(t *testing.T) {
	m := &mockScaleOperation{}
	as := New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: time.Minute}, m, nopLogger())
	as.RegisterFunction("fn", nil)
	markFunctionIdle(t, as, "fn")

	err := as.ScaleUpWhenReady(context.TODO(), "fn")
	assert.NoError(t, err)
	assert.Equal(t, 0, m.UpCalls())

	err = as.ScaleDownWhenIdle(context.TODO(), "fn")
	assert.NoError(t, err)
	assert.Equal(t, 0, m.DownCalls())
	state, _ := as.GetState("fn")
	assert.Equal(t, StateActive, state)
}

func TestConcurrentInvocationAccounting(t *testing.T) {
	as := New(Config{Enabled: true, Platform: "tinyfaas", DefaultIdleDuration: time.Minute}, &mockScaleOperation{}, nopLogger())
	as.RegisterFunction("fn", nil)

	assert.NoError(t, as.StartInvocation("fn"))
	assert.NoError(t, as.StartInvocation("fn"))
	assert.NoError(t, as.StartInvocation("fn"))

	status := as.GetFunctionStatus()["fn"]
	assert.Equal(t, StateBlocked, status.State)
	assert.Equal(t, 3, status.InFlight)

	as.EndInvocation("fn")
	as.EndInvocation("fn")
	status = as.GetFunctionStatus()["fn"]
	assert.Equal(t, StateBlocked, status.State)
	assert.Equal(t, 1, status.InFlight)

	as.EndInvocation("fn")
	status = as.GetFunctionStatus()["fn"]
	assert.Equal(t, StateActive, status.State)
	assert.Equal(t, 0, status.InFlight)
}

func TestScaleUpWhenReadyWaitsForScalingDown(t *testing.T) {
	downStarted := make(chan struct{})
	downRelease := make(chan struct{})
	m := &mockScaleOperation{downCh: downStarted, downReleaseCh: downRelease}
	as := New(Config{Enabled: true, Platform: "tinyfaas", DefaultIdleDuration: time.Minute}, m, nopLogger())
	as.RegisterFunction("fn", nil)
	markFunctionIdle(t, as, "fn")

	downDone := make(chan error, 1)
	go func() {
		downDone <- as.ScaleDownWhenIdle(context.TODO(), "fn")
	}()

	<-downStarted

	upDone := make(chan error, 1)
	go func() {
		upDone <- as.ScaleUpWhenReady(context.TODO(), "fn")
	}()

	select {
	case err := <-upDone:
		t.Fatalf("scale-up should wait for scale-down to complete, got %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(downRelease)
	assert.NoError(t, <-downDone)
	assert.NoError(t, <-upDone)
	assert.Equal(t, 1, m.DownCalls())
	assert.Equal(t, 1, m.UpCalls())
	state, _ := as.GetState("fn")
	assert.Equal(t, StateActive, state)
}

func TestBeginInvocationDone(t *testing.T) {
	as := New(Config{Enabled: true, Platform: "tinyfaas", DefaultIdleDuration: time.Minute}, &mockScaleOperation{}, nopLogger())
	as.RegisterFunction("fn", nil)

	done, err := as.BeginInvocation("fn")
	assert.NoError(t, err)
	state, _ := as.GetState("fn")
	assert.Equal(t, StateBlocked, state)

	done()
	state, _ = as.GetState("fn")
	assert.Equal(t, StateActive, state)
}

func TestMonitorScalesDownIdleActiveOnly(t *testing.T) {
	m := &mockScaleOperation{}
	as := New(Config{Enabled: true, Platform: "tinyfaas", CheckInterval: 20 * time.Millisecond, DefaultIdleDuration: 30 * time.Millisecond}, m, nopLogger())
	as.RegisterFunction("fn", nil)

	as.Start()
	defer as.Stop()
	time.Sleep(120 * time.Millisecond)

	assert.Equal(t, 1, m.DownCalls())
	state, _ := as.GetState("fn")
	assert.Equal(t, StateScaledDown, state)
}
