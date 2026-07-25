package dialer_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vozko/domain/calls/billing"
	"vozko/domain/conversation"
	"vozko/domain/dialer"
)

type fakeCRMCall struct {
	id          string
	audio       chan []byte
	events      chan conversation.CallEvent
	done        chan struct{}
	hangupCalls int32
}

func newFakeCall(id string) *fakeCRMCall {
	return &fakeCRMCall{
		id:     id,
		audio:  make(chan []byte, 4),
		events: make(chan conversation.CallEvent, 4),
		done:   make(chan struct{}),
	}
}
func (c *fakeCRMCall) ID() string                            { return c.id }
func (c *fakeCRMCall) SendAudio([]byte) error                { return nil }
func (c *fakeCRMCall) AudioStream() <-chan []byte            { return c.audio }
func (c *fakeCRMCall) Events() <-chan conversation.CallEvent { return c.events }
func (c *fakeCRMCall) Done() <-chan struct{}                 { return c.done }
func (c *fakeCRMCall) Hangup() error {
	atomic.AddInt32(&c.hangupCalls, 1)
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	return nil
}
func (c *fakeCRMCall) HangupCount() int32 { return atomic.LoadInt32(&c.hangupCalls) }

type fakeBalanceChecker struct {
	mu      sync.Mutex
	balance int64
	err     error
	calls   int32
}

func (f *fakeBalanceChecker) HasSufficientBalance(string, int64) (bool, error) { return true, nil }
func (f *fakeBalanceChecker) Invalidate(string)                                {}
func (f *fakeBalanceChecker) InvalidateDebounced(string)                       {}
func (f *fakeBalanceChecker) GetBalance(string) (int64, error) {
	atomic.AddInt32(&f.calls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return 0, f.err
	}
	return f.balance, nil
}

type fakeInflightReserver struct {
	mu           sync.Mutex
	totals       map[string]int64
	reserveErr   error
	allowReserve bool
	reserveCalls int32
	releaseCalls int32
	refreshCalls int32
}

func newFakeReserver() *fakeInflightReserver {
	return &fakeInflightReserver{totals: make(map[string]int64), allowReserve: true}
}
func (f *fakeInflightReserver) Reserve(ws string, delta, budget int64) (bool, error) {
	atomic.AddInt32(&f.reserveCalls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reserveErr != nil {
		return false, f.reserveErr
	}
	if !f.allowReserve {
		return false, nil
	}
	if f.totals[ws]+delta > budget {
		return false, nil
	}
	f.totals[ws] += delta
	return true, nil
}
func (f *fakeInflightReserver) Release(ws string, delta int64) error {
	atomic.AddInt32(&f.releaseCalls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.totals[ws] -= delta
	return nil
}
func (f *fakeInflightReserver) RefreshTTL(string, time.Duration) error {
	atomic.AddInt32(&f.refreshCalls, 1)
	return nil
}
func (f *fakeInflightReserver) GetInflight(ws string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.totals[ws], nil
}

type fakeBillingPub struct {
	mu       sync.Mutex
	topic    string
	messages [][]byte
	err      error
}

func (f *fakeBillingPub) Publish(topic string, msg []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.topic = topic
	cp := make([]byte, len(msg))
	copy(cp, msg)
	f.messages = append(f.messages, cp)
	return nil
}
func (f *fakeBillingPub) PublishWithDelay(topic string, msg []byte, _ time.Duration) error {
	return f.Publish(topic, msg)
}
func (f *fakeBillingPub) ValidateConnection() error { return nil }
func (f *fakeBillingPub) Count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.messages)
}
func (f *fakeBillingPub) LastEvent(t *testing.T) billing.CallCompletedEvent {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.messages) == 0 {
		t.Fatal("no billing events published")
	}
	var ev billing.CallCompletedEvent
	if err := json.Unmarshal(f.messages[len(f.messages)-1], &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return ev
}

type fakeAdmission struct {
	mu           sync.Mutex
	lease        *dialer.CallAdmissionLease
	releaseCalls int32
	lastReleased *dialer.CallAdmissionLease
	releaseErr   error
}

func (f *fakeAdmission) Acquire(context.Context, dialer.CallAdmissionInput) (*dialer.CallAdmissionLease, error) {
	return f.lease, nil
}
func (f *fakeAdmission) Refresh(*dialer.CallAdmissionLease, time.Duration) error { return nil }
func (f *fakeAdmission) Release(l *dialer.CallAdmissionLease) error {
	atomic.AddInt32(&f.releaseCalls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastReleased = l
	return f.releaseErr
}

func quietLogger() *log.Logger { return log.New(io.Discard, "", 0) }

func newRunner(
	admission dialer.CallAdmissionCoordinator,
	reserver *fakeInflightReserver,
	checker *fakeBalanceChecker,
	pub *fakeBillingPub,
) *OutboundCallLifecycleRunner {
	return NewOutboundCallLifecycleRunner(admission, checker, reserver, pub, quietLogger())
}

func TestLifecycleRun_PublishesBillingOnNaturalHangup(t *testing.T) {
	call := newFakeCall("call-billing")
	admission := &fakeAdmission{}
	reserver := newFakeReserver()
	checker := &fakeBalanceChecker{balance: 1_000_000}
	pub := &fakeBillingPub{}

	startedAt := time.Now().Add(-45 * time.Second)
	runner := newRunner(admission, reserver, checker, pub)

	var endedReason string
	var endedDuration time.Duration
	done := make(chan struct{})
	go func() {
		runner.Run(context.Background(), OutboundCallLifecycleInput{
			Call:        call,
			WorkspaceID: "ws-1",
			StartedAt:   startedAt,
			Admission:   &dialer.CallAdmissionLease{WorkspaceID: "ws-1", ReservedMicros: 100, PerMinuteCostMicros: 100},
			OnEnded: func(reason string, d time.Duration) {
				endedReason = reason
				endedDuration = d
			},
		})
		close(done)
	}()

	_ = call.Hangup()
	<-done

	if endedReason != "ended" {
		t.Fatalf("reason = %q, want 'ended'", endedReason)
	}
	if endedDuration <= 0 {
		t.Fatalf("duration must be positive, got %v", endedDuration)
	}
	if pub.Count() != 1 {
		t.Fatalf("billing events = %d, want 1", pub.Count())
	}
	ev := pub.LastEvent(t)
	if ev.CallID != "call-billing" || ev.WorkspaceID != "ws-1" {
		t.Fatalf("event mismatch: %+v", ev)
	}
	if ev.CallSource != billing.CallSourceWebSocket {
		t.Fatalf("CallSource = %q, want websocket", ev.CallSource)
	}
	if ev.DurationSec <= 0 {
		t.Fatalf("DurationSec must be > 0, got %d", ev.DurationSec)
	}
	if atomic.LoadInt32(&admission.releaseCalls) != 1 {
		t.Fatalf("admission Release calls = %d, want 1", admission.releaseCalls)
	}
}

func TestLifecycleRun_TerminalEventEndsCallAndPublishesBilling(t *testing.T) {
	call := newFakeCall("call-terminal")
	admission := &fakeAdmission{}
	reserver := newFakeReserver()
	checker := &fakeBalanceChecker{balance: 1_000_000}
	pub := &fakeBillingPub{}
	runner := newRunner(admission, reserver, checker, pub)

	startedAt := time.Now().Add(-10 * time.Second)
	endedCh := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		runner.Run(context.Background(), OutboundCallLifecycleInput{
			Call:        call,
			WorkspaceID: "ws-1",
			StartedAt:   startedAt,
			Admission:   &dialer.CallAdmissionLease{WorkspaceID: "ws-1", ReservedMicros: 50, PerMinuteCostMicros: 50},
			OnEnded:     func(reason string, _ time.Duration) { endedCh <- reason },
		})
		close(done)
	}()

	call.events <- conversation.CallEvent{Type: "ended", Reason: "peer_hangup"}

	select {
	case r := <-endedCh:
		if r != "ended" {
			t.Fatalf("reason = %q, want 'ended'", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnEnded never called")
	}
	<-done
	if pub.Count() != 1 {
		t.Fatalf("billing events = %d, want 1", pub.Count())
	}
}

func TestLifecycleRun_ForwardsStatusAndAudio(t *testing.T) {
	call := newFakeCall("call-stream")
	admission := &fakeAdmission{}
	reserver := newFakeReserver()
	checker := &fakeBalanceChecker{balance: 1_000_000}
	pub := &fakeBillingPub{}
	runner := newRunner(admission, reserver, checker, pub)

	var statusCount, audioBytes int32
	endedCh := make(chan struct{})
	go runner.Run(context.Background(), OutboundCallLifecycleInput{
		Call:        call,
		WorkspaceID: "ws-1",
		StartedAt:   time.Now(),
		Admission:   &dialer.CallAdmissionLease{WorkspaceID: "ws-1"},
		OnStatus: func(conversation.CallEvent) {
			atomic.AddInt32(&statusCount, 1)
		},
		OnAudio: func(pcm []byte) {
			atomic.AddInt32(&audioBytes, int32(len(pcm)))
		},
		OnEnded: func(string, time.Duration) { close(endedCh) },
	})

	call.events <- conversation.CallEvent{Type: "ringing"}
	call.events <- conversation.CallEvent{Type: "answered"}
	call.audio <- []byte{1, 2, 3, 4}
	call.audio <- []byte{5, 6}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&statusCount) >= 2 && atomic.LoadInt32(&audioBytes) == 6 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	_ = call.Hangup()
	<-endedCh

	if atomic.LoadInt32(&statusCount) < 2 {
		t.Fatalf("status events forwarded = %d, want >= 2", statusCount)
	}
	if atomic.LoadInt32(&audioBytes) != 6 {
		t.Fatalf("audio bytes forwarded = %d, want 6", audioBytes)
	}
}

func TestLifecycleRun_ExtendsReservationViaBalanceGuard(t *testing.T) {
	call := newFakeCall("call-extend")
	admission := &fakeAdmission{}
	reserver := newFakeReserver()
	checker := &fakeBalanceChecker{balance: 10_000_000}
	pub := &fakeBillingPub{}
	runner := newRunner(admission, reserver, checker, pub)
	runner.SetBalanceGuardInterval(20 * time.Millisecond)

	startedAt := time.Unix(0, 0)
	var nowNano atomic.Int64

	nowNano.Store(startedAt.Add(70 * time.Second).UnixNano())
	runner.SetNowFn(func() time.Time { return time.Unix(0, nowNano.Load()) })

	done := make(chan struct{})
	go func() {
		runner.Run(context.Background(), OutboundCallLifecycleInput{
			Call:        call,
			WorkspaceID: "ws-1",
			StartedAt:   startedAt,
			Admission: &dialer.CallAdmissionLease{
				WorkspaceID:         "ws-1",
				PerMinuteCostMicros: 100,
				ReservedMicros:      100,
			},
			OnEnded: func(string, time.Duration) {},
		})
		close(done)
	}()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&reserver.reserveCalls) >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	_ = call.Hangup()
	<-done

	if atomic.LoadInt32(&reserver.reserveCalls) < 1 {
		t.Fatalf("expected at least one Reserve call, got %d", reserver.reserveCalls)
	}
	if atomic.LoadInt32(&reserver.refreshCalls) < 1 {
		t.Fatalf("expected RefreshTTL after successful reserve, got %d", reserver.refreshCalls)
	}

	if admission.lastReleased == nil {
		t.Fatal("expected lease release")
	}
	if admission.lastReleased.ReservedMicros < 200 {
		t.Fatalf("lease reservation on release = %d, want >= 200 (1 min initial + 1 min extension)", admission.lastReleased.ReservedMicros)
	}
}

func TestLifecycleRun_InsufficientBalanceHangsUpAndStillBills(t *testing.T) {
	call := newFakeCall("call-insuf")
	admission := &fakeAdmission{}
	reserver := newFakeReserver()
	reserver.allowReserve = false
	checker := &fakeBalanceChecker{balance: 0}
	pub := &fakeBillingPub{}
	runner := newRunner(admission, reserver, checker, pub)
	runner.SetBalanceGuardInterval(10 * time.Millisecond)

	startedAt := time.Unix(0, 0)
	var nowNano atomic.Int64
	nowNano.Store(startedAt.Add(90 * time.Second).UnixNano())
	runner.SetNowFn(func() time.Time { return time.Unix(0, nowNano.Load()) })

	reasonCh := make(chan string, 1)
	go runner.Run(context.Background(), OutboundCallLifecycleInput{
		Call:        call,
		WorkspaceID: "ws-1",
		StartedAt:   startedAt,
		Admission: &dialer.CallAdmissionLease{
			WorkspaceID:         "ws-1",
			PerMinuteCostMicros: 100,
			ReservedMicros:      100,
		},
		OnEnded: func(reason string, _ time.Duration) { reasonCh <- reason },
	})

	select {
	case r := <-reasonCh:
		if r != "insufficient_balance" {
			t.Fatalf("reason = %q, want 'insufficient_balance'", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnEnded never called")
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && call.HangupCount() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if call.HangupCount() == 0 {
		t.Fatal("expected call.Hangup() on insufficient balance")
	}

	if pub.Count() != 1 {
		t.Fatalf("billing events = %d, want 1", pub.Count())
	}
}

func TestLifecycleRun_BalanceErrorsFailClosedAfter3(t *testing.T) {
	call := newFakeCall("call-failclosed")
	admission := &fakeAdmission{}
	reserver := newFakeReserver()
	checker := &fakeBalanceChecker{err: errors.New("redis down")}
	pub := &fakeBillingPub{}
	runner := newRunner(admission, reserver, checker, pub)
	runner.SetBalanceGuardInterval(10 * time.Millisecond)

	startedAt := time.Unix(0, 0)
	var nowNano atomic.Int64
	nowNano.Store(startedAt.Add(90 * time.Second).UnixNano())
	runner.SetNowFn(func() time.Time { return time.Unix(0, nowNano.Load()) })

	reasonCh := make(chan string, 1)
	go runner.Run(context.Background(), OutboundCallLifecycleInput{
		Call:        call,
		WorkspaceID: "ws-1",
		StartedAt:   startedAt,
		Admission: &dialer.CallAdmissionLease{
			WorkspaceID:         "ws-1",
			PerMinuteCostMicros: 100,
			ReservedMicros:      100,
		},
		OnEnded: func(reason string, _ time.Duration) { reasonCh <- reason },
	})

	select {
	case r := <-reasonCh:
		if r != "balance_check_error" {
			t.Fatalf("reason = %q, want 'balance_check_error'", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("OnEnded never called")
	}
	if atomic.LoadInt32(&checker.calls) < balanceGuardFailClosedThreshold {
		t.Fatalf("balance GetBalance calls = %d, want >= %d", checker.calls, balanceGuardFailClosedThreshold)
	}
}

func TestLifecycleRun_ReserveErrorsFailClosedAfter3(t *testing.T) {
	call := newFakeCall("call-reserveerr")
	admission := &fakeAdmission{}
	reserver := newFakeReserver()
	reserver.reserveErr = errors.New("redis pool exhausted")
	checker := &fakeBalanceChecker{balance: 1_000_000}
	pub := &fakeBillingPub{}
	runner := newRunner(admission, reserver, checker, pub)
	runner.SetBalanceGuardInterval(10 * time.Millisecond)

	startedAt := time.Unix(0, 0)
	current := startedAt.Add(90 * time.Second)
	runner.SetNowFn(func() time.Time { return current })

	reasonCh := make(chan string, 1)
	go runner.Run(context.Background(), OutboundCallLifecycleInput{
		Call:        call,
		WorkspaceID: "ws-1",
		StartedAt:   startedAt,
		Admission: &dialer.CallAdmissionLease{
			WorkspaceID:         "ws-1",
			PerMinuteCostMicros: 100,
			ReservedMicros:      100,
		},
		OnEnded: func(reason string, _ time.Duration) { reasonCh <- reason },
	})

	select {
	case r := <-reasonCh:
		if r != "balance_check_error" {
			t.Fatalf("reason = %q, want 'balance_check_error'", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("OnEnded never called")
	}
}

func TestLifecycleRun_NoBillingPubIsNoOp(t *testing.T) {
	call := newFakeCall("call-nopub")
	admission := &fakeAdmission{}
	reserver := newFakeReserver()
	checker := &fakeBalanceChecker{balance: 1_000_000}
	runner := NewOutboundCallLifecycleRunner(admission, checker, reserver, nil, quietLogger())

	endedCh := make(chan struct{})
	go runner.Run(context.Background(), OutboundCallLifecycleInput{
		Call:        call,
		WorkspaceID: "ws-1",
		StartedAt:   time.Now().Add(-5 * time.Second),
		Admission:   &dialer.CallAdmissionLease{WorkspaceID: "ws-1"},
		OnEnded:     func(string, time.Duration) { close(endedCh) },
	})

	_ = call.Hangup()
	<-endedCh

	if atomic.LoadInt32(&admission.releaseCalls) != 1 {
		t.Fatalf("admission releaseCalls = %d, want 1", admission.releaseCalls)
	}
}

func TestLifecycleRun_ZeroDurationSkipsBilling(t *testing.T) {
	call := newFakeCall("call-zero")
	admission := &fakeAdmission{}
	reserver := newFakeReserver()
	checker := &fakeBalanceChecker{balance: 1_000_000}
	pub := &fakeBillingPub{}
	runner := newRunner(admission, reserver, checker, pub)

	frozen := time.Now()
	runner.SetNowFn(func() time.Time { return frozen })

	endedCh := make(chan struct{})
	go runner.Run(context.Background(), OutboundCallLifecycleInput{
		Call:        call,
		WorkspaceID: "ws-1",
		StartedAt:   frozen,
		Admission:   &dialer.CallAdmissionLease{WorkspaceID: "ws-1"},
		OnEnded:     func(string, time.Duration) { close(endedCh) },
	})
	_ = call.Hangup()
	<-endedCh

	if pub.Count() != 0 {
		t.Fatalf("expected 0 billing events for zero-duration call, got %d", pub.Count())
	}
}

func TestLifecycleRun_ContextCancelEndsCallAndReleases(t *testing.T) {
	call := newFakeCall("call-ctx")
	admission := &fakeAdmission{}
	reserver := newFakeReserver()
	checker := &fakeBalanceChecker{balance: 1_000_000}
	pub := &fakeBillingPub{}
	runner := newRunner(admission, reserver, checker, pub)

	ctx, cancel := context.WithCancel(context.Background())
	reasonCh := make(chan string, 1)
	go runner.Run(ctx, OutboundCallLifecycleInput{
		Call:        call,
		WorkspaceID: "ws-1",
		StartedAt:   time.Now().Add(-5 * time.Second),
		Admission:   &dialer.CallAdmissionLease{WorkspaceID: "ws-1"},
		OnEnded:     func(reason string, _ time.Duration) { reasonCh <- reason },
	})
	cancel()
	select {
	case r := <-reasonCh:
		if r != "cancelled" {
			t.Fatalf("reason = %q, want 'cancelled'", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnEnded never called after cancel")
	}
	if atomic.LoadInt32(&admission.releaseCalls) != 1 {
		t.Fatal("admission not released on ctx cancel")
	}
}

func TestLifecycleRun_NilCallIsNoop(t *testing.T) {
	runner := NewOutboundCallLifecycleRunner(nil, nil, nil, nil, quietLogger())

	done := make(chan struct{})
	go func() {
		runner.Run(context.Background(), OutboundCallLifecycleInput{Call: nil})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run with nil call did not return")
	}
}

func TestLifecycleRun_NilCallWithAdmissionReleasesLease(t *testing.T) {
	admission := &fakeAdmission{}
	reserver := newFakeReserver()
	checker := &fakeBalanceChecker{balance: 1_000_000}
	pub := &fakeBillingPub{}
	runner := newRunner(admission, reserver, checker, pub)

	lease := &dialer.CallAdmissionLease{WorkspaceID: "ws-1", ReservedMicros: 100, PerMinuteCostMicros: 100}
	done := make(chan struct{})
	go func() {
		runner.Run(context.Background(), OutboundCallLifecycleInput{
			Call:        nil,
			WorkspaceID: "ws-1",
			StartedAt:   time.Now(),
			Admission:   lease,
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run with nil call + admission did not return")
	}

	if atomic.LoadInt32(&admission.releaseCalls) != 1 {
		t.Fatalf("admission releaseCalls = %d, want 1 (nil call must still release lease)", admission.releaseCalls)
	}
	if pub.Count() != 0 {
		t.Fatalf("expected 0 billing events for nil call, got %d", pub.Count())
	}
}

func TestLifecycleRun_BillingPublishErrorDoesNotPanic(t *testing.T) {
	call := newFakeCall("call-puberr")
	admission := &fakeAdmission{}
	reserver := newFakeReserver()
	checker := &fakeBalanceChecker{balance: 1_000_000}
	pub := &fakeBillingPub{err: errors.New("queue down")}
	runner := newRunner(admission, reserver, checker, pub)

	endedCh := make(chan struct{})
	go runner.Run(context.Background(), OutboundCallLifecycleInput{
		Call:        call,
		WorkspaceID: "ws-1",
		StartedAt:   time.Now().Add(-10 * time.Second),
		Admission:   &dialer.CallAdmissionLease{WorkspaceID: "ws-1"},
		OnEnded:     func(string, time.Duration) { close(endedCh) },
	})
	_ = call.Hangup()
	<-endedCh

	if atomic.LoadInt32(&admission.releaseCalls) != 1 {
		t.Fatal("admission not released after publish error")
	}
}

func TestLifecycleRun_BillingDurationMatchesElapsedExactly(t *testing.T) {
	call := newFakeCall("call-duration")
	admission := &fakeAdmission{}
	reserver := newFakeReserver()
	checker := &fakeBalanceChecker{balance: 1_000_000}
	pub := &fakeBillingPub{}
	runner := newRunner(admission, reserver, checker, pub)

	startedAt := time.Unix(0, 0)
	endAt := startedAt.Add(125 * time.Second)
	runner.SetNowFn(func() time.Time { return endAt })

	done := make(chan struct{})
	go func() {
		runner.Run(context.Background(), OutboundCallLifecycleInput{
			Call:        call,
			WorkspaceID: "ws-1",
			StartedAt:   startedAt,
			Admission:   &dialer.CallAdmissionLease{WorkspaceID: "ws-1"},
			OnEnded:     func(string, time.Duration) {},
		})
		close(done)
	}()
	_ = call.Hangup()
	<-done

	if pub.Count() != 1 {
		t.Fatalf("billing publishes = %d, want 1", pub.Count())
	}
	ev := pub.LastEvent(t)
	if ev.DurationSec != 125 {
		t.Fatalf("DurationSec = %d, want exactly 125 (nowFn - StartedAt)", ev.DurationSec)
	}
	if !ev.CallEnd.Equal(endAt) {
		t.Fatalf("CallEnd = %v, want %v (must equal nowFn at publish time)", ev.CallEnd, endAt)
	}
	if !ev.CallStart.Equal(startedAt) {
		t.Fatalf("CallStart = %v, want %v (must equal input.StartedAt)", ev.CallStart, startedAt)
	}
}

func TestLifecycleRun_PanicInOnAudioStillReleasesAndPublishes(t *testing.T) {
	call := newFakeCall("call-panic-audio")
	admission := &fakeAdmission{}
	reserver := newFakeReserver()
	checker := &fakeBalanceChecker{balance: 1_000_000}
	pub := &fakeBillingPub{}
	runner := newRunner(admission, reserver, checker, pub)

	startedAt := time.Now().Add(-30 * time.Second)

	done := make(chan struct{})
	var recovered any
	go func() {
		defer func() {
			recovered = recover()
			close(done)
		}()
		runner.Run(context.Background(), OutboundCallLifecycleInput{
			Call:        call,
			WorkspaceID: "ws-panic",
			StartedAt:   startedAt,
			Admission:   &dialer.CallAdmissionLease{WorkspaceID: "ws-panic", ReservedMicros: 100, PerMinuteCostMicros: 100},
			OnAudio: func([]byte) {
				panic("simulated codec failure")
			},
		})
	}()

	call.audio <- []byte{0x01, 0x02}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after OnAudio panic")
	}

	if recovered == nil {
		t.Fatal("expected panic to propagate out of Run (deferred cleanup must still execute, but the panic itself MUST NOT be silently swallowed)")
	}

	if got := atomic.LoadInt32(&admission.releaseCalls); got != 1 {
		t.Fatalf("admission Release calls = %d after panic, want 1 (defer chain must run on panic)", got)
	}
	if pub.Count() != 1 {
		t.Fatalf("billing publishes = %d after panic, want 1 (defer chain must run on panic)", pub.Count())
	}
	ev := pub.LastEvent(t)
	if ev.WorkspaceID != "ws-panic" || ev.CallID != "call-panic-audio" {
		t.Fatalf("billing event mismatch after panic: %+v", ev)
	}
	if ev.DurationSec <= 0 {
		t.Fatalf("DurationSec = %d after panic, want > 0", ev.DurationSec)
	}
}

func TestLifecycleRun_PanicInOnStatusStillReleasesAndPublishes(t *testing.T) {
	call := newFakeCall("call-panic-status")
	admission := &fakeAdmission{}
	reserver := newFakeReserver()
	checker := &fakeBalanceChecker{balance: 1_000_000}
	pub := &fakeBillingPub{}
	runner := newRunner(admission, reserver, checker, pub)

	startedAt := time.Now().Add(-30 * time.Second)

	done := make(chan struct{})
	var recovered any
	go func() {
		defer func() {
			recovered = recover()
			close(done)
		}()
		runner.Run(context.Background(), OutboundCallLifecycleInput{
			Call:        call,
			WorkspaceID: "ws-panic-status",
			StartedAt:   startedAt,
			Admission:   &dialer.CallAdmissionLease{WorkspaceID: "ws-panic-status", ReservedMicros: 100, PerMinuteCostMicros: 100},
			OnStatus: func(conversation.CallEvent) {
				panic("simulated status handler bug")
			},
		})
	}()

	call.events <- conversation.CallEvent{Type: "ringing"}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after OnStatus panic")
	}

	if recovered == nil {
		t.Fatal("expected panic to propagate (panic must not be silently swallowed by Run)")
	}
	if got := atomic.LoadInt32(&admission.releaseCalls); got != 1 {
		t.Fatalf("admission Release calls = %d after panic, want 1", got)
	}
	if pub.Count() != 1 {
		t.Fatalf("billing publishes = %d after panic, want 1", pub.Count())
	}
}
