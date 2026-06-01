package autoscaler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func nopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// mockScaleOperation supports gating both ScaleUp and ScaleDown so tests can
// deterministically exercise the transient ScalingUp/ScalingDown states.
type mockScaleOperation struct {
	mu sync.Mutex

	downCalls int
	upCalls   int

	downErr error
	upErr   error

	downDelay time.Duration
	upDelay   time.Duration

	// Optional gating channels. When non-nil, the corresponding scale
	// operation will:
	//   1. close <opDelivered> on entry (so tests know it has started),
	//   2. block on <-opRelease> until the test releases it.
	downCh        chan struct{}
	downReleaseCh chan struct{}
	upCh          chan struct{}
	upReleaseCh   chan struct{}

	// Concurrency tracking for worker-pool tests. Updated atomically in
	// ScaleDown only.
	downConcurrentNow  atomic.Int32
	downConcurrentPeak atomic.Int32
}

func (m *mockScaleOperation) ScaleDown(functionName string) error {
	m.mu.Lock()
	m.downCalls++
	delay := m.downDelay
	err := m.downErr
	downCh := m.downCh
	release := m.downReleaseCh
	m.mu.Unlock()

	now := m.downConcurrentNow.Add(1)
	defer m.downConcurrentNow.Add(-1)
	for {
		peak := m.downConcurrentPeak.Load()
		if now <= peak || m.downConcurrentPeak.CompareAndSwap(peak, now) {
			break
		}
	}

	if downCh != nil {
		closeOnce(downCh)
	}
	if release != nil {
		<-release
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	return err
}

func (m *mockScaleOperation) DownConcurrentPeak() int32 {
	return m.downConcurrentPeak.Load()
}

func (m *mockScaleOperation) ScaleUp(functionName string) error {
	m.mu.Lock()
	m.upCalls++
	delay := m.upDelay
	err := m.upErr
	upCh := m.upCh
	release := m.upReleaseCh
	m.mu.Unlock()

	if upCh != nil {
		closeOnce(upCh)
	}
	if release != nil {
		<-release
	}
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

// closeOnce closes a chan struct{} idempotently (used by mocks where multiple
// invocations of ScaleUp/ScaleDown might happen across reconfigured runs).
func closeOnce(ch chan struct{}) {
	defer func() { _ = recover() }()
	close(ch)
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

// scaleDownIfStillIdleByName is a test helper that exercises the monitor-side
// idle-aware scale-down primitive. It mirrors what runs from inside the
// monitor goroutine but without spinning up a real ticker.
func (as *AutoScaler) scaleDownIfStillIdleByName(name string) (bool, *functionEntry, error) {
	entry, ok := as.getFunctionEntry(name)
	if !ok {
		return false, nil, ErrFunctionNotFound
	}
	scaled, err := as.scaleDownIfStillIdle(context.Background(), name, entry)
	return scaled, entry, err
}

// waitForState polls until the function reaches the desired state or the
// deadline expires. Used in tests where deterministic synchronization isn't
// possible without exposing more internals.
func waitForState(t *testing.T, as *AutoScaler, name string, want LifecycleState, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if state, ok := as.GetState(name); ok && state == want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	state, _ := as.GetState(name)
	t.Fatalf("timed out waiting for %s to reach %s (current=%s)", name, want, state)
}

// waitForCondition polls until predicate returns true or the deadline expires.
func waitForCondition(t *testing.T, predicate func() bool, timeout time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for condition: %s", msg)
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

	err := as.StartInvocation(context.Background(), "fn")
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

	require.NoError(t, as.StartInvocation(context.Background(), "fn"))

	done := make(chan error, 1)
	go func() {
		done <- as.ScaleDownWhenIdle(context.Background(), "fn")
	}()

	// Spin briefly to confirm scale-down hasn't fired while we're Blocked.
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, 0, m.DownCalls())

	as.EndInvocation("fn")
	require.NoError(t, <-done)
	// ScaleDownWhenIdle is unconditional - once the in-flight request
	// finishes and we transition back to Active, the scale-down fires.
	assert.Equal(t, 1, m.DownCalls())
	state, _ := as.GetState("fn")
	assert.Equal(t, StateScaledDown, state)
}

func TestEnsureActiveFromScaledDown(t *testing.T) {
	m := &mockScaleOperation{}
	as := New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: time.Minute}, m, nopLogger())
	as.RegisterFunctionWithState("fn", nil, StateScaledDown)

	err := as.ScaleUpWhenReady(context.Background(), "fn")
	assert.NoError(t, err)
	assert.Equal(t, 1, m.UpCalls())
	state, _ := as.GetState("fn")
	assert.Equal(t, StateActive, state)
}

func TestEnsureActiveConcurrentCollapsesScaleUp(t *testing.T) {
	m := &mockScaleOperation{upDelay: 50 * time.Millisecond}
	as := New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: time.Minute}, m, nopLogger())
	as.RegisterFunctionWithState("fn", nil, StateScaledDown)

	err1 := make(chan error, 1)
	err2 := make(chan error, 1)
	go func() { err1 <- as.ScaleUpWhenReady(context.Background(), "fn") }()
	go func() { err2 <- as.ScaleUpWhenReady(context.Background(), "fn") }()

	assert.NoError(t, <-err1)
	assert.NoError(t, <-err2)
	assert.Equal(t, 1, m.UpCalls())
}

func TestScaleDownFailureReturnsActive(t *testing.T) {
	m := &mockScaleOperation{downErr: errors.New("boom")}
	as := New(Config{Enabled: true, Platform: "tinyfaas", DefaultIdleDuration: time.Minute}, m, nopLogger())
	as.RegisterFunction("fn", nil)
	markFunctionIdle(t, as, "fn")

	err := as.ScaleDownWhenIdle(context.Background(), "fn")
	assert.Error(t, err)
	state, _ := as.GetState("fn")
	assert.Equal(t, StateActive, state)
}

func TestScaleUpFailureReturnsScaledDown(t *testing.T) {
	m := &mockScaleOperation{upErr: errors.New("boom")}
	as := New(Config{Enabled: true, Platform: "tinyfaas", DefaultIdleDuration: time.Minute}, m, nopLogger())
	as.RegisterFunctionWithState("fn", nil, StateScaledDown)

	err := as.ScaleUpWhenReady(context.Background(), "fn")
	assert.Error(t, err)
	state, _ := as.GetState("fn")
	assert.Equal(t, StateScaledDown, state)
}

func TestScaleUpWhenReadyBlockedReturnsImmediately(t *testing.T) {
	m := &mockScaleOperation{}
	as := New(Config{Enabled: true, Platform: "tinyfaas", DefaultIdleDuration: time.Minute}, m, nopLogger())
	as.RegisterFunction("fn", nil)

	require.NoError(t, as.StartInvocation(context.Background(), "fn"))
	err := as.ScaleUpWhenReady(context.Background(), "fn")
	assert.NoError(t, err)
	assert.Equal(t, 0, m.UpCalls())

	as.EndInvocation("fn")
}

func TestScaleUpWhenReadyRefreshesActiveFunctionActivity(t *testing.T) {
	m := &mockScaleOperation{}
	as := New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: time.Minute}, m, nopLogger())
	as.RegisterFunction("fn", nil)
	markFunctionIdle(t, as, "fn")

	// ScaleUpWhenReady on an Active function refreshes LastActiveTime
	// without triggering a scale operation.
	err := as.ScaleUpWhenReady(context.Background(), "fn")
	assert.NoError(t, err)
	assert.Equal(t, 0, m.UpCalls())

	// The monitor's idle check must respect the refreshed timestamp and
	// NOT scale down. (Direct calls to ScaleDownWhenIdle are unconditional
	// and would scale down regardless - that's tested separately.)
	scaledDown, _, err := as.scaleDownIfStillIdleByName("fn")
	assert.NoError(t, err)
	assert.False(t, scaledDown, "monitor should not scale down recently-active function")
	assert.Equal(t, 0, m.DownCalls())
	state, _ := as.GetState("fn")
	assert.Equal(t, StateActive, state)
}

func TestConcurrentInvocationAccounting(t *testing.T) {
	as := New(Config{Enabled: true, Platform: "tinyfaas", DefaultIdleDuration: time.Minute}, &mockScaleOperation{}, nopLogger())
	as.RegisterFunction("fn", nil)

	require.NoError(t, as.StartInvocation(context.Background(), "fn"))
	require.NoError(t, as.StartInvocation(context.Background(), "fn"))
	require.NoError(t, as.StartInvocation(context.Background(), "fn"))

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
		downDone <- as.ScaleDownWhenIdle(context.Background(), "fn")
	}()

	<-downStarted

	upDone := make(chan error, 1)
	go func() {
		upDone <- as.ScaleUpWhenReady(context.Background(), "fn")
	}()

	select {
	case err := <-upDone:
		t.Fatalf("scale-up should wait for scale-down to complete, got %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(downRelease)
	require.NoError(t, <-downDone)
	require.NoError(t, <-upDone)
	assert.Equal(t, 1, m.DownCalls())
	assert.Equal(t, 1, m.UpCalls())
	state, _ := as.GetState("fn")
	assert.Equal(t, StateActive, state)
}

func TestBeginInvocationDone(t *testing.T) {
	as := New(Config{Enabled: true, Platform: "tinyfaas", DefaultIdleDuration: time.Minute}, &mockScaleOperation{}, nopLogger())
	as.RegisterFunction("fn", nil)

	done, err := as.BeginInvocation(context.Background(), "fn")
	require.NoError(t, err)
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

	waitForState(t, as, "fn", StateScaledDown, 1*time.Second)
	assert.Equal(t, 1, m.DownCalls())
}

func TestStopIsIdempotent(t *testing.T) {
	as := New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: time.Minute, CheckInterval: time.Second}, &mockScaleOperation{}, nopLogger())
	as.Start()

	as.Stop()
	// Second Stop must not panic and must return immediately.
	done := make(chan struct{})
	go func() {
		as.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("second Stop blocked")
	}
}

func TestStopWithoutStart(t *testing.T) {
	as := New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: time.Minute, CheckInterval: time.Second}, &mockScaleOperation{}, nopLogger())

	done := make(chan struct{})
	go func() {
		as.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop deadlocked when Start was never called")
	}
}

func TestStartIsIdempotent(t *testing.T) {
	m := &mockScaleOperation{}
	as := New(Config{Enabled: true, Platform: "faasd", CheckInterval: 20 * time.Millisecond, DefaultIdleDuration: 30 * time.Millisecond}, m, nopLogger())
	as.RegisterFunction("fn", nil)

	// Multiple Start calls should be safe and not spawn extra monitors.
	as.Start()
	as.Start()
	as.Start()
	defer as.Stop()

	waitForState(t, as, "fn", StateScaledDown, 1*time.Second)
	// One scale-down call regardless of how many times Start was called.
	assert.Equal(t, 1, m.DownCalls())
}

func TestStopUnblocksStartInvocationWaiters(t *testing.T) {
	upStarted := make(chan struct{})
	upRelease := make(chan struct{})
	m := &mockScaleOperation{upCh: upStarted, upReleaseCh: upRelease}
	as := New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: time.Minute, CheckInterval: time.Hour}, m, nopLogger())
	as.RegisterFunctionWithState("fn", nil, StateScaledDown)

	// Drive the function into ScalingUp via a backgrounded ScaleUp.
	scaleUpDone := make(chan error, 1)
	go func() { scaleUpDone <- as.ScaleUpWhenReady(context.Background(), "fn") }()
	<-upStarted

	// A subsequent StartInvocation should now block waiting for the
	// transient ScalingUp state to clear.
	startDone := make(chan error, 1)
	go func() { startDone <- as.StartInvocation(context.Background(), "fn") }()

	// Confirm it's actually blocked (not yet returned).
	select {
	case err := <-startDone:
		t.Fatalf("StartInvocation should block during ScalingUp, returned %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	// Stop should release waiters.
	stopDone := make(chan struct{})
	go func() {
		// We can't actually finish Stop until the monitor goroutine is
		// done - but Enabled=true && started=false means Stop closes
		// doneChan eagerly. We never started, so this is fast.
		as.Stop()
		close(stopDone)
	}()

	select {
	case err := <-startDone:
		require.ErrorIs(t, err, ErrAutoscalerStopped)
	case <-time.After(time.Second):
		t.Fatal("StartInvocation did not return after Stop()")
	}

	<-stopDone

	// Cleanup: release the still-running ScaleUp goroutine.
	close(upRelease)
	<-scaleUpDone
}

func TestStopUnblocksScaleDownWaiters(t *testing.T) {
	m := &mockScaleOperation{}
	as := New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: time.Minute, CheckInterval: time.Hour}, m, nopLogger())
	as.RegisterFunction("fn", nil)
	markFunctionIdle(t, as, "fn")

	// Block scale-down by holding an in-flight invocation.
	require.NoError(t, as.StartInvocation(context.Background(), "fn"))

	scaleDownDone := make(chan error, 1)
	go func() { scaleDownDone <- as.ScaleDownWhenIdle(context.Background(), "fn") }()

	// Confirm it's blocked.
	select {
	case err := <-scaleDownDone:
		t.Fatalf("ScaleDownWhenIdle should block while Blocked, returned %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	as.Stop()

	select {
	case err := <-scaleDownDone:
		require.ErrorIs(t, err, ErrAutoscalerStopped)
	case <-time.After(time.Second):
		t.Fatal("ScaleDownWhenIdle did not return after Stop()")
	}

	// Cleanup.
	as.EndInvocation("fn")
}

func TestStartInvocationCtxCancel(t *testing.T) {
	upStarted := make(chan struct{})
	upRelease := make(chan struct{})
	m := &mockScaleOperation{upCh: upStarted, upReleaseCh: upRelease}
	as := New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: time.Minute, CheckInterval: time.Hour}, m, nopLogger())
	as.RegisterFunctionWithState("fn", nil, StateScaledDown)

	scaleUpDone := make(chan error, 1)
	go func() { scaleUpDone <- as.ScaleUpWhenReady(context.Background(), "fn") }()
	<-upStarted

	ctx, cancel := context.WithCancel(context.Background())
	startDone := make(chan error, 1)
	go func() { startDone <- as.StartInvocation(ctx, "fn") }()

	// Ensure StartInvocation has actually entered the wait.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-startDone:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("StartInvocation did not return after ctx cancel")
	}

	// Cleanup.
	close(upRelease)
	<-scaleUpDone
}

func TestScaleUpWhenReadyCtxAlreadyCanceled(t *testing.T) {
	upStarted := make(chan struct{})
	upRelease := make(chan struct{})
	m := &mockScaleOperation{upCh: upStarted, upReleaseCh: upRelease}
	as := New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: time.Minute, CheckInterval: time.Hour}, m, nopLogger())
	as.RegisterFunctionWithState("fn", nil, StateScaledDown)

	// Hold the function in ScalingUp.
	first := make(chan error, 1)
	go func() { first <- as.ScaleUpWhenReady(context.Background(), "fn") }()
	<-upStarted

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := as.ScaleUpWhenReady(ctx, "fn")
	require.ErrorIs(t, err, context.Canceled)

	// Cleanup.
	close(upRelease)
	<-first
}

func TestScaleUpWhenReadyCtxCancelDuringWait(t *testing.T) {
	downStarted := make(chan struct{})
	downRelease := make(chan struct{})
	m := &mockScaleOperation{downCh: downStarted, downReleaseCh: downRelease}
	as := New(Config{Enabled: true, Platform: "tinyfaas", DefaultIdleDuration: time.Minute, CheckInterval: time.Hour}, m, nopLogger())
	as.RegisterFunction("fn", nil)
	markFunctionIdle(t, as, "fn")

	// Drive into ScalingDown.
	downDone := make(chan error, 1)
	go func() { downDone <- as.ScaleDownWhenIdle(context.Background(), "fn") }()
	<-downStarted

	// Now ScaleUpWhenReady will block waiting for ScalingDown to clear.
	ctx, cancel := context.WithCancel(context.Background())
	upDone := make(chan error, 1)
	go func() { upDone <- as.ScaleUpWhenReady(ctx, "fn") }()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-upDone:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("ScaleUpWhenReady did not return after ctx cancel")
	}

	// Cleanup.
	close(downRelease)
	require.NoError(t, <-downDone)
}

func TestScaleDownWhenIdleCtxCancel(t *testing.T) {
	m := &mockScaleOperation{}
	as := New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: time.Minute, CheckInterval: time.Hour}, m, nopLogger())
	as.RegisterFunction("fn", nil)
	markFunctionIdle(t, as, "fn")
	require.NoError(t, as.StartInvocation(context.Background(), "fn"))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- as.ScaleDownWhenIdle(ctx, "fn") }()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("ScaleDownWhenIdle did not return after ctx cancel")
	}

	// Cleanup.
	as.EndInvocation("fn")
}

func TestUnregisterReleasesStartInvocationWaiter(t *testing.T) {
	upStarted := make(chan struct{})
	upRelease := make(chan struct{})
	m := &mockScaleOperation{upCh: upStarted, upReleaseCh: upRelease}
	as := New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: time.Minute, CheckInterval: time.Hour}, m, nopLogger())
	as.RegisterFunctionWithState("fn", nil, StateScaledDown)

	first := make(chan error, 1)
	go func() { first <- as.ScaleUpWhenReady(context.Background(), "fn") }()
	<-upStarted

	startDone := make(chan error, 1)
	go func() { startDone <- as.StartInvocation(context.Background(), "fn") }()

	time.Sleep(20 * time.Millisecond)
	as.UnregisterFunction("fn")

	select {
	case err := <-startDone:
		require.ErrorIs(t, err, ErrFunctionUnregistered)
	case <-time.After(time.Second):
		t.Fatal("StartInvocation did not return after UnregisterFunction")
	}

	// Cleanup.
	close(upRelease)
	<-first
}

func TestUnregisterReleasesScaleDownWaiter(t *testing.T) {
	m := &mockScaleOperation{}
	as := New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: time.Minute, CheckInterval: time.Hour}, m, nopLogger())
	as.RegisterFunction("fn", nil)
	markFunctionIdle(t, as, "fn")
	require.NoError(t, as.StartInvocation(context.Background(), "fn"))

	done := make(chan error, 1)
	go func() { done <- as.ScaleDownWhenIdle(context.Background(), "fn") }()

	time.Sleep(20 * time.Millisecond)
	as.UnregisterFunction("fn")

	select {
	case err := <-done:
		require.ErrorIs(t, err, ErrFunctionUnregistered)
	case <-time.After(time.Second):
		t.Fatal("ScaleDownWhenIdle did not return after UnregisterFunction")
	}
}

func TestReRegisterReleasesOldWaiters(t *testing.T) {
	upStarted := make(chan struct{})
	upRelease := make(chan struct{})
	m := &mockScaleOperation{upCh: upStarted, upReleaseCh: upRelease}
	as := New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: time.Minute, CheckInterval: time.Hour}, m, nopLogger())
	as.RegisterFunctionWithState("fn", nil, StateScaledDown)

	first := make(chan error, 1)
	go func() { first <- as.ScaleUpWhenReady(context.Background(), "fn") }()
	<-upStarted

	waitDone := make(chan error, 1)
	go func() { waitDone <- as.StartInvocation(context.Background(), "fn") }()

	time.Sleep(20 * time.Millisecond)

	// Re-register the same name with a fresh entry.
	as.RegisterFunctionWithState("fn", nil, StateActive)

	select {
	case err := <-waitDone:
		require.ErrorIs(t, err, ErrFunctionUnregistered)
	case <-time.After(time.Second):
		t.Fatal("Old waiter did not return after re-register")
	}

	// Cleanup.
	close(upRelease)
	<-first
}

func TestStartInvocationOnUnregisteredFunction(t *testing.T) {
	as := New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: time.Minute}, &mockScaleOperation{}, nopLogger())
	err := as.StartInvocation(context.Background(), "missing")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrFunctionNotFound)
}

func TestStartInvocationOnScaledDown(t *testing.T) {
	as := New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: time.Minute}, &mockScaleOperation{}, nopLogger())
	as.RegisterFunctionWithState("fn", nil, StateScaledDown)

	err := as.StartInvocation(context.Background(), "fn")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrFunctionScaledDown)
}

func TestRegisterFunctionInvalidStateFallsBack(t *testing.T) {
	as := New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: time.Minute}, &mockScaleOperation{}, nopLogger())
	as.RegisterFunctionWithState("fn", nil, StateScalingUp)
	state, ok := as.GetState("fn")
	require.True(t, ok)
	assert.Equal(t, StateActive, state)
}

func TestBeginInvocationDoneCalledTwice(t *testing.T) {
	as := New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: time.Minute}, &mockScaleOperation{}, nopLogger())
	as.RegisterFunction("fn", nil)

	done, err := as.BeginInvocation(context.Background(), "fn")
	require.NoError(t, err)

	done()
	done()

	state, _ := as.GetState("fn")
	assert.Equal(t, StateActive, state)
	status := as.GetFunctionStatus()["fn"]
	assert.Equal(t, 0, status.InFlight, "second done() must not double-decrement")
}

func TestBeginInvocationDoneFromMultipleGoroutines(t *testing.T) {
	as := New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: time.Minute}, &mockScaleOperation{}, nopLogger())
	as.RegisterFunction("fn", nil)

	// Start two invocations: each returns its own done callback.
	done1, err := as.BeginInvocation(context.Background(), "fn")
	require.NoError(t, err)
	done2, err := as.BeginInvocation(context.Background(), "fn")
	require.NoError(t, err)

	// Spawn many goroutines hammering both done callbacks. All must
	// observe net InFlight=0 at the end with no data race (-race).
	const goroutines = 16
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			done1()
			done2()
		}()
	}
	wg.Wait()

	status := as.GetFunctionStatus()["fn"]
	assert.Equal(t, 0, status.InFlight)
	assert.Equal(t, StateActive, status.State)
}

func TestMonitorDoesNotStallOnBlockedFunction(t *testing.T) {
	m := &mockScaleOperation{}
	as := New(Config{Enabled: true, Platform: "faasd", CheckInterval: 20 * time.Millisecond, DefaultIdleDuration: 30 * time.Millisecond}, m, nopLogger())

	// "stuck" is registered, marked idle, but held in Blocked by an
	// in-flight invocation that never ends. The monitor must NOT block
	// indefinitely waiting for it.
	as.RegisterFunction("stuck", nil)
	require.NoError(t, as.StartInvocation(context.Background(), "stuck"))
	markFunctionIdle(t, as, "stuck")

	// "idle" is just sitting active, due to be scaled down on the next tick.
	as.RegisterFunction("idle", nil)

	as.Start()
	defer as.Stop()

	waitForState(t, as, "idle", StateScaledDown, 2*time.Second)
	// "stuck" must remain Blocked (still holds the invocation).
	state, _ := as.GetState("stuck")
	assert.Equal(t, StateBlocked, state)

	// Cleanup.
	as.EndInvocation("stuck")
}

func TestNewLowercasesPlatform(t *testing.T) {
	as := New(Config{Enabled: true, Platform: "FAASD", DefaultIdleDuration: time.Minute}, &mockScaleOperation{}, nopLogger())
	as.RegisterFunction("fn", map[string]string{"com.openfaas.scale.zero": "true"})

	status := as.GetFunctionStatus()["fn"]
	assert.True(t, status.ScaleToZeroEnabled, "labels should be honored regardless of platform casing")
}

func TestNewWithNilScaleOpDisabledIsSafe(t *testing.T) {
	as := New(Config{Enabled: false, Platform: "faasd", DefaultIdleDuration: time.Minute}, nil, nopLogger())

	// All methods should be no-op safe when disabled.
	as.RegisterFunction("fn", nil)
	require.NoError(t, as.StartInvocation(context.Background(), "fn"))
	as.EndInvocation("fn")
	require.NoError(t, as.ScaleUpWhenReady(context.Background(), "fn"))
	require.NoError(t, as.ScaleDownWhenIdle(context.Background(), "fn"))
	as.RecordActivity("fn")
	as.UnregisterFunction("fn")
	as.Start()
	as.Stop()
}

func TestNewWithNilScaleOpEnabledPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil ScaleOperation with Enabled=true")
		}
	}()
	_ = New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: time.Minute}, nil, nopLogger())
}

// TestStressConcurrentInvocations spawns many goroutines hammering Start/End
// on a single function. With the lock-and-channel design, all races must
// resolve cleanly: state ends Active, InFlight==0.
func TestStressConcurrentInvocations(t *testing.T) {
	as := New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: time.Minute, CheckInterval: time.Hour}, &mockScaleOperation{}, nopLogger())
	as.RegisterFunction("fn", nil)

	const goroutines = 50
	const itersPerG = 20

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < itersPerG; j++ {
				if err := as.StartInvocation(context.Background(), "fn"); err != nil {
					t.Errorf("StartInvocation: %v", err)
					return
				}
				as.EndInvocation("fn")
			}
		}()
	}
	wg.Wait()

	status := as.GetFunctionStatus()["fn"]
	assert.Equal(t, StateActive, status.State)
	assert.Equal(t, 0, status.InFlight)
}

// TestStressConcurrentScaleUpAndInvocations mixes ScaleUpWhenReady with
// Start/End. From StateScaledDown, only one ScaleUp call should happen even
// under heavy concurrency.
func TestStressConcurrentScaleUpAndInvocations(t *testing.T) {
	m := &mockScaleOperation{upDelay: 10 * time.Millisecond}
	as := New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: time.Minute, CheckInterval: time.Hour}, m, nopLogger())
	as.RegisterFunctionWithState("fn", nil, StateScaledDown)

	const goroutines = 30
	var wg sync.WaitGroup
	wg.Add(goroutines)

	// First half: scale-up callers.
	for i := 0; i < goroutines/2; i++ {
		go func() {
			defer wg.Done()
			if err := as.ScaleUpWhenReady(context.Background(), "fn"); err != nil {
				t.Errorf("ScaleUpWhenReady: %v", err)
			}
		}()
	}
	// Second half: invocation callers (will get ErrFunctionScaledDown if
	// they race the scale-up - that's fine; we count successes).
	var startSuccess atomic.Int64
	for i := 0; i < goroutines/2; i++ {
		go func() {
			defer wg.Done()
			// Retry briefly to give the scale-up time to win the race.
			deadline := time.Now().Add(500 * time.Millisecond)
			for time.Now().Before(deadline) {
				err := as.StartInvocation(context.Background(), "fn")
				if err == nil {
					startSuccess.Add(1)
					as.EndInvocation("fn")
					return
				}
				if !errors.Is(err, ErrFunctionScaledDown) {
					t.Errorf("unexpected StartInvocation err: %v", err)
					return
				}
				time.Sleep(2 * time.Millisecond)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, 1, m.UpCalls(), "expected single-flight scale-up")
	state, _ := as.GetState("fn")
	assert.Equal(t, StateActive, state)
	assert.Greater(t, int(startSuccess.Load()), 0, "at least one StartInvocation should have succeeded post-ScaleUp")
}

// TestStressConcurrentScaleDownAndInvocations mixes ScaleDownWhenIdle with
// Start/End. We assert the FSM invariants hold at the end: if Active then
// InFlight==0; if Blocked then InFlight>0; ScaledDown means no inflight.
func TestStressConcurrentScaleDownAndInvocations(t *testing.T) {
	m := &mockScaleOperation{}
	as := New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: time.Hour, CheckInterval: time.Hour}, m, nopLogger())
	as.RegisterFunction("fn", nil)

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				if err := as.StartInvocation(context.Background(), "fn"); err == nil {
					time.Sleep(time.Millisecond)
					as.EndInvocation("fn")
				}
			}
		}()
	}

	// One goroutine repeatedly tries ScaleDown with a short ctx so it
	// never wedges the test if Blocked persists.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
			_ = as.ScaleDownWhenIdle(ctx, "fn")
			cancel()
			time.Sleep(2 * time.Millisecond)
		}
	}()

	wg.Wait()

	// At end, ensure invariants.
	status := as.GetFunctionStatus()["fn"]
	switch status.State {
	case StateActive:
		assert.Equal(t, 0, status.InFlight)
	case StateBlocked:
		assert.Greater(t, status.InFlight, 0)
	case StateScaledDown:
		assert.Equal(t, 0, status.InFlight)
	default:
		t.Fatalf("unexpected terminal state %s", status.State)
	}
}

// TestNoGoroutineLeakOnUnregister verifies that all waiters terminate when
// the function is unregistered
func TestNoGoroutineLeakOnUnregister(t *testing.T) {
	upStarted := make(chan struct{})
	upRelease := make(chan struct{})
	m := &mockScaleOperation{upCh: upStarted, upReleaseCh: upRelease}
	as := New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: time.Minute, CheckInterval: time.Hour}, m, nopLogger())
	as.RegisterFunctionWithState("fn", nil, StateScaledDown)

	// Drive into ScalingUp (held by upRelease).
	scaleUpDone := make(chan error, 1)
	go func() { scaleUpDone <- as.ScaleUpWhenReady(context.Background(), "fn") }()
	<-upStarted

	const waiters = 20
	errs := make(chan error, waiters)
	for i := 0; i < waiters; i++ {
		go func() { errs <- as.StartInvocation(context.Background(), "fn") }()
	}

	// Give goroutines time to enter the wait state.
	time.Sleep(30 * time.Millisecond)

	baselineGoroutines := runtime.NumGoroutine()

	as.UnregisterFunction("fn")

	for i := 0; i < waiters; i++ {
		select {
		case err := <-errs:
			require.ErrorIs(t, err, ErrFunctionUnregistered)
		case <-time.After(time.Second):
			t.Fatalf("waiter %d did not return", i)
		}
	}

	// Cleanup the held ScaleUp.
	close(upRelease)
	<-scaleUpDone

	// All waiters returned; goroutine count must shrink back near
	// baseline (allow small slack for runtime workers).
	waitForCondition(t, func() bool {
		return runtime.NumGoroutine() <= baselineGoroutines
	}, time.Second, "goroutines did not return to baseline after unregister")
}

// TestRecordActivityPreventsScaleDown verifies that periodic heartbeats keep
// a function from being scaled down by the monitor.
func TestRecordActivityPreventsScaleDown(t *testing.T) {
	m := &mockScaleOperation{}
	as := New(Config{Enabled: true, Platform: "faasd", CheckInterval: 10 * time.Millisecond, DefaultIdleDuration: 50 * time.Millisecond}, m, nopLogger())
	as.RegisterFunction("fn", nil)

	as.Start()
	defer as.Stop()

	// Heartbeat for 150ms - longer than the idle duration. The function
	// must never be scaled down.
	deadline := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(deadline) {
		as.RecordActivity("fn")
		time.Sleep(10 * time.Millisecond)
	}

	assert.Equal(t, 0, m.DownCalls())
	state, _ := as.GetState("fn")
	assert.Equal(t, StateActive, state)
}

// TestRecordActivityBatch is a smoke test for the batch heartbeat path.
func TestRecordActivityBatch(t *testing.T) {
	as := New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: time.Minute, CheckInterval: time.Hour}, &mockScaleOperation{}, nopLogger())
	as.RegisterFunction("a", nil)
	as.RegisterFunction("b", nil)

	statusBefore := as.GetFunctionStatus()
	time.Sleep(2 * time.Millisecond)
	as.RecordActivityBatch([]string{"a", "b", "missing"})

	statusAfter := as.GetFunctionStatus()
	assert.True(t, statusAfter["a"].LastAccessTime.After(statusBefore["a"].LastAccessTime))
	assert.True(t, statusAfter["b"].LastAccessTime.After(statusBefore["b"].LastAccessTime))
}

// TestIsEnabledIsScalingDownGetIdleTime exercises the simple inspector
// methods that previously had zero coverage.
func TestInspectorMethods(t *testing.T) {
	disabled := New(Config{Enabled: false, Platform: "faasd", DefaultIdleDuration: time.Minute}, nil, nopLogger())
	assert.False(t, disabled.IsEnabled())
	assert.False(t, disabled.IsScaledDown("fn"))
	assert.False(t, disabled.IsScalingDown("fn"))
	assert.Zero(t, disabled.GetIdleTime("fn"))

	as := New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: time.Minute, CheckInterval: time.Hour}, &mockScaleOperation{}, nopLogger())
	assert.True(t, as.IsEnabled())
	as.RegisterFunction("fn", nil)
	assert.False(t, as.IsScaledDown("fn"))
	assert.False(t, as.IsScalingDown("fn"))
	idle := as.GetIdleTime("fn")
	assert.GreaterOrEqual(t, idle, time.Duration(0))
}

// TestParseScaleConfigDisabledLabel ensures explicit "false" disables scale
// to zero on a per-function basis.
func TestParseScaleConfigDisabledLabel(t *testing.T) {
	as := New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: time.Minute}, &mockScaleOperation{}, nopLogger())
	as.RegisterFunction("fn", map[string]string{"com.openfaas.scale.zero": "false"})

	status := as.GetFunctionStatus()["fn"]
	assert.False(t, status.ScaleToZeroEnabled)
}

// TestParseScaleConfigInvalidIdleDurationFallsBack ensures malformed labels
// don't propagate errors and instead fall back to the default.
func TestParseScaleConfigInvalidIdleDurationFallsBack(t *testing.T) {
	as := New(Config{Enabled: true, Platform: "faasd", DefaultIdleDuration: 7 * time.Minute}, &mockScaleOperation{}, nopLogger())
	as.RegisterFunction("fn", map[string]string{
		"com.openfaas.scale.zero":               "true",
		"com.openfaas.scale.zero.idle_duration": "garbage",
	})

	status := as.GetFunctionStatus()["fn"]
	assert.Equal(t, 7*time.Minute, status.IdleDuration)
}

// ------- Forced ScaleDownWhenIdle vs. monitor freshness tests -------

// TestScaleDownWhenIdleIsUnconditional pins the contract: callers using
// ScaleDownWhenIdle have already decided to scale down (e.g. redeploy or
// explicit replicas=0), and recent activity must NOT cancel that. The monitor
// has its own freshness check.
func TestScaleDownWhenIdleIsUnconditional(t *testing.T) {
	m := &mockScaleOperation{}
	as := New(Config{Enabled: true, Platform: "tinyfaas", DefaultIdleDuration: time.Hour}, m, nopLogger())
	as.RegisterFunction("fn", map[string]string{"com.tinyfaas.scale.zero": "true"})

	// Function has fresh activity (just registered).
	require.NoError(t, as.ScaleDownWhenIdle(context.Background(), "fn"))
	assert.Equal(t, 1, m.DownCalls())
	state, _ := as.GetState("fn")
	assert.Equal(t, StateScaledDown, state)
}

// TestScaleDownWhenIdleAfterEndInvocation reproduces the tinyFaaS redeploy
// scenario: an in-flight request blocks a forced scale-down; once the request
// finishes, the scale-down must fire (it must NOT short-circuit because the
// EndInvocation just refreshed LastActiveTime).
func TestScaleDownWhenIdleAfterEndInvocation(t *testing.T) {
	m := &mockScaleOperation{}
	as := New(Config{Enabled: true, Platform: "tinyfaas", DefaultIdleDuration: time.Hour}, m, nopLogger())
	as.RegisterFunction("fn", map[string]string{"com.tinyfaas.scale.zero": "true"})

	require.NoError(t, as.StartInvocation(context.Background(), "fn"))

	done := make(chan error, 1)
	go func() { done <- as.ScaleDownWhenIdle(context.Background(), "fn") }()

	// Confirm the scale-down is parked while we hold the invocation.
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, 0, m.DownCalls())

	as.EndInvocation("fn")

	require.NoError(t, <-done)
	assert.Equal(t, 1, m.DownCalls(), "forced scale-down must fire even though EndInvocation just refreshed LastActiveTime")
	state, _ := as.GetState("fn")
	assert.Equal(t, StateScaledDown, state)
}

// TestMonitorRespectsActivityRefresh verifies the monitor-side path still
// respects fresh activity (so the new unconditional ScaleDownWhenIdle doesn't
// produce an aggressive monitor).
func TestMonitorRespectsActivityRefresh(t *testing.T) {
	m := &mockScaleOperation{}
	as := New(Config{Enabled: true, Platform: "tinyfaas", CheckInterval: 10 * time.Millisecond, DefaultIdleDuration: 30 * time.Millisecond}, m, nopLogger())
	as.RegisterFunction("fn", nil)

	as.Start()
	defer as.Stop()

	// Heartbeat for longer than the idle window. The monitor should never
	// scale us down because LastActiveTime keeps moving forward.
	deadline := time.Now().Add(120 * time.Millisecond)
	for time.Now().Before(deadline) {
		as.RecordActivity("fn")
		time.Sleep(5 * time.Millisecond)
	}

	assert.Equal(t, 0, m.DownCalls())
	state, _ := as.GetState("fn")
	assert.Equal(t, StateActive, state)
}

// TestMonitorSkipsBlockedFunctions verifies the monitor does not block when
// a function is currently serving an in-flight request. It returns
// immediately so other functions can be checked; the next tick will retry.
func TestMonitorSkipsBlockedFunctions(t *testing.T) {
	m := &mockScaleOperation{}
	as := New(Config{Enabled: true, Platform: "tinyfaas", DefaultIdleDuration: 30 * time.Millisecond}, m, nopLogger())
	as.RegisterFunction("fn", map[string]string{"com.tinyfaas.scale.zero": "true"})
	markFunctionIdle(t, as, "fn")
	require.NoError(t, as.StartInvocation(context.Background(), "fn"))

	// Drive the monitor's idle-aware scale-down primitive directly. With
	// the function in Blocked, it must return immediately with
	// scaledDown=false rather than waiting for the invocation to finish.
	startTime := time.Now()
	scaled, _, err := as.scaleDownIfStillIdleByName("fn")
	elapsed := time.Since(startTime)

	require.NoError(t, err)
	assert.False(t, scaled)
	assert.Less(t, elapsed, 50*time.Millisecond, "monitor must not park on a Blocked function")
	assert.Equal(t, 0, m.DownCalls())

	// Cleanup.
	as.EndInvocation("fn")
}

// TestMonitorRespectsActivityRefreshAfterUnblock verifies that once a
// function returns to Active with a freshly-refreshed LastActiveTime, the
// monitor sees the freshness and skips the scale-down on its next visit.
func TestMonitorRespectsActivityRefreshAfterUnblock(t *testing.T) {
	m := &mockScaleOperation{}
	as := New(Config{Enabled: true, Platform: "tinyfaas", DefaultIdleDuration: 30 * time.Millisecond}, m, nopLogger())
	as.RegisterFunction("fn", map[string]string{"com.tinyfaas.scale.zero": "true"})
	markFunctionIdle(t, as, "fn")

	// Simulate "request just landed and finished": Active -> Blocked -> Active
	// with a fresh LastActiveTime.
	require.NoError(t, as.StartInvocation(context.Background(), "fn"))
	as.EndInvocation("fn")

	scaled, _, err := as.scaleDownIfStillIdleByName("fn")
	require.NoError(t, err)
	assert.False(t, scaled, "monitor must not scale down a function that just finished a request")
	assert.Equal(t, 0, m.DownCalls())
}

// ------- Worker pool tests for the parallel idle scan -------

// registerIdleFunctions registers N idle scale-to-zero-enabled functions
// with names "fnK" for K in [0,N). Returns the list of names for convenience.
func registerIdleFunctions(t *testing.T, as *AutoScaler, n int) []string {
	t.Helper()
	names := make([]string, n)
	for i := 0; i < n; i++ {
		names[i] = "fn" + strconvI(i)
		as.RegisterFunction(names[i], map[string]string{"com.tinyfaas.scale.zero": "true"})
		markFunctionIdle(t, as, names[i])
	}
	return names
}

// strconvI is a tiny inline int->string to avoid the strconv import churn
// across the file.
func strconvI(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}

// TestMonitorRespectsConcurrencyLimit asserts that the worker pool never
// dispatches more concurrent ScaleDown calls than configured.
func TestMonitorRespectsConcurrencyLimit(t *testing.T) {
	release := make(chan struct{})
	m := &mockScaleOperation{downReleaseCh: release}
	as := New(Config{
		Enabled:                 true,
		Platform:                "tinyfaas",
		DefaultIdleDuration:     time.Minute,
		CheckInterval:           time.Hour, // we trigger checkIdleFunctions manually
		MaxConcurrentScaleDowns: 2,
	}, m, nopLogger())

	const numFns = 6
	registerIdleFunctions(t, as, numFns)

	// Run the scan in a background goroutine; close release once the peak
	// has had enough time to climb to the configured limit, then wait.
	scanDone := make(chan struct{})
	go func() {
		as.checkIdleFunctions()
		close(scanDone)
	}()

	// Wait for at least 2 concurrent calls to land.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if m.downConcurrentNow.Load() >= 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}

	// Hold long enough for any extra workers to (incorrectly) land if the
	// limit wasn't honored.
	time.Sleep(50 * time.Millisecond)
	peak := m.DownConcurrentPeak()
	close(release)
	<-scanDone

	assert.LessOrEqual(t, peak, int32(2), "peak concurrent ScaleDown must not exceed MaxConcurrentScaleDowns")
	assert.Equal(t, numFns, m.DownCalls(), "every idle function must eventually be scaled down")
}

// TestMonitorParallelScaleDownReachesPeak asserts that the worker pool DOES
// achieve the configured concurrency when there's enough work for it.
func TestMonitorParallelScaleDownReachesPeak(t *testing.T) {
	release := make(chan struct{})
	m := &mockScaleOperation{downReleaseCh: release}
	as := New(Config{
		Enabled:                 true,
		Platform:                "tinyfaas",
		DefaultIdleDuration:     time.Minute,
		CheckInterval:           time.Hour,
		MaxConcurrentScaleDowns: 4,
	}, m, nopLogger())
	registerIdleFunctions(t, as, 8)

	scanDone := make(chan struct{})
	go func() {
		as.checkIdleFunctions()
		close(scanDone)
	}()

	// Wait for 4 concurrent calls to actually land in the mock.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if m.downConcurrentNow.Load() >= 4 {
			break
		}
		time.Sleep(time.Millisecond)
	}

	assert.GreaterOrEqual(t, m.downConcurrentNow.Load(), int32(4), "worker pool should saturate to MaxConcurrentScaleDowns")
	close(release)
	<-scanDone
	assert.Equal(t, 8, m.DownCalls())
}

// TestMonitorWorkerPoolDefault verifies that an unspecified
// MaxConcurrentScaleDowns defaults to DEFAULT_MAX_CONCURRENT_SCALE_DOWNS.
func TestMonitorWorkerPoolDefault(t *testing.T) {
	release := make(chan struct{})
	m := &mockScaleOperation{downReleaseCh: release}
	as := New(Config{
		Enabled:             true,
		Platform:            "tinyfaas",
		DefaultIdleDuration: time.Minute,
		CheckInterval:       time.Hour,
		// MaxConcurrentScaleDowns not set -> default 4.
	}, m, nopLogger())
	require.Equal(t, DEFAULT_MAX_CONCURRENT_SCALE_DOWNS, as.config.MaxConcurrentScaleDowns)

	registerIdleFunctions(t, as, 8)

	scanDone := make(chan struct{})
	go func() {
		as.checkIdleFunctions()
		close(scanDone)
	}()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if m.downConcurrentNow.Load() >= int32(DEFAULT_MAX_CONCURRENT_SCALE_DOWNS) {
			break
		}
		time.Sleep(time.Millisecond)
	}

	// Allow brief settle to ensure peak is recorded.
	time.Sleep(20 * time.Millisecond)
	peak := m.DownConcurrentPeak()
	close(release)
	<-scanDone

	assert.Equal(t, int32(DEFAULT_MAX_CONCURRENT_SCALE_DOWNS), peak,
		"default pool size should match DEFAULT_MAX_CONCURRENT_SCALE_DOWNS")
}

// TestMonitorParallelLatency verifies that a tick with W functions of equal
// duration completes in roughly one duration when MaxConcurrentScaleDowns >= W
// (i.e., the workers actually run in parallel rather than sequentially).
func TestMonitorParallelLatency(t *testing.T) {
	const each = 60 * time.Millisecond
	m := &mockScaleOperation{downDelay: each}
	as := New(Config{
		Enabled:                 true,
		Platform:                "tinyfaas",
		DefaultIdleDuration:     time.Minute,
		CheckInterval:           time.Hour,
		MaxConcurrentScaleDowns: 4,
	}, m, nopLogger())
	registerIdleFunctions(t, as, 4)

	start := time.Now()
	as.checkIdleFunctions()
	elapsed := time.Since(start)

	// Sequential would be 4*each = 240ms. Parallel should be ~each = 60ms.
	// Allow generous slack to avoid flakiness on busy CI.
	assert.Less(t, elapsed, 3*each,
		"parallel scan should be much faster than sequential; got %v", elapsed)
	assert.Equal(t, 4, m.DownCalls())
}

// TestMonitorEmptyFunctionsTickIsNoOp confirms that an empty registry
// produces no work and no goroutine churn.
func TestMonitorEmptyFunctionsTickIsNoOp(t *testing.T) {
	m := &mockScaleOperation{}
	as := New(Config{
		Enabled:                 true,
		Platform:                "tinyfaas",
		DefaultIdleDuration:     time.Minute,
		CheckInterval:           time.Hour,
		MaxConcurrentScaleDowns: 4,
	}, m, nopLogger())

	baseline := runtime.NumGoroutine()
	as.checkIdleFunctions()
	// Allow brief settle.
	time.Sleep(20 * time.Millisecond)

	assert.Equal(t, 0, m.DownCalls())
	assert.LessOrEqual(t, runtime.NumGoroutine(), baseline+1,
		"empty tick must not leak goroutines")
}

// TestStopWaitsForInFlightWorkers verifies that Stop() does not return until
// all currently-running scale-down workers have actually exited. This relies
// on the scaleDownWG join inside Stop().
func TestStopWaitsForInFlightWorkers(t *testing.T) {
	release := make(chan struct{})
	m := &mockScaleOperation{downReleaseCh: release}
	as := New(Config{
		Enabled:                 true,
		Platform:                "tinyfaas",
		CheckInterval:           5 * time.Millisecond,
		DefaultIdleDuration:     time.Millisecond,
		MaxConcurrentScaleDowns: 2,
	}, m, nopLogger())
	registerIdleFunctions(t, as, 2)

	as.Start()

	// Wait until both workers are actually inside ScaleDown.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if m.downConcurrentNow.Load() >= 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	require.Equal(t, int32(2), m.downConcurrentNow.Load(), "both workers should be in flight")

	stopReturned := make(chan struct{})
	go func() {
		as.Stop()
		close(stopReturned)
	}()

	// Stop() must NOT return while workers are blocked on release.
	select {
	case <-stopReturned:
		t.Fatal("Stop() returned while workers were still in flight")
	case <-time.After(50 * time.Millisecond):
	}

	// Release the workers; Stop() must now return promptly.
	close(release)
	select {
	case <-stopReturned:
	case <-time.After(time.Second):
		t.Fatal("Stop() did not return after workers were released")
	}
	assert.Equal(t, int32(0), m.downConcurrentNow.Load(), "no workers should remain in-flight after Stop")
}

// TestMonitorTickOverlapSkipped exercises the tickInProgress guard: a slow
// scan must not be re-entered by a subsequent tick while it is still
// running. We check this by counting how many scale-down attempts land on
// each function; with tick overlap the count would exceed the function
// count.
func TestMonitorTickOverlapSkipped(t *testing.T) {
	release := make(chan struct{})
	m := &mockScaleOperation{downReleaseCh: release}
	as := New(Config{
		Enabled:                 true,
		Platform:                "tinyfaas",
		CheckInterval:           5 * time.Millisecond, // fast ticks
		DefaultIdleDuration:     time.Millisecond,
		MaxConcurrentScaleDowns: 1, // one worker -> deterministic in-flight=1
	}, m, nopLogger())
	registerIdleFunctions(t, as, 1)

	as.Start()

	// Wait for the first ScaleDown call to land.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if m.downConcurrentNow.Load() >= 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	require.Equal(t, int32(1), m.downConcurrentNow.Load())

	// Let many ticks fire while the scan is held open. None of them
	// should be able to enter checkIdleFunctions, so DownCalls must
	// stay at exactly 1.
	time.Sleep(80 * time.Millisecond)
	assert.Equal(t, 1, m.DownCalls(),
		"tickInProgress guard must prevent overlapping idle scans")

	close(release)
	as.Stop()
}
