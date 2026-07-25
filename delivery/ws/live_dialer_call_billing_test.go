package ws

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vozko/domain/calls/billing"
	"vozko/domain/conversation"
	dialer_domain "vozko/domain/dialer"
	dialer_usecase "vozko/usecases/dialer"
)

type recordingEndUseCase struct {
	admission *recordingAdmission
}

func (r *recordingEndUseCase) Execute(_ context.Context, input dialer_domain.EndOutboundCallInput) error {
	if input.ReleaseAdmission && input.Admission != nil && r.admission != nil {
		_ = r.admission.Release(input.Admission)
	}
	return nil
}

type liveDialerCallBillingFakePub struct {
	mu       sync.Mutex
	messages [][]byte
}

func (f *liveDialerCallBillingFakePub) Publish(_ string, msg []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]byte, len(msg))
	copy(cp, msg)
	f.messages = append(f.messages, cp)
	return nil
}

func (f *liveDialerCallBillingFakePub) PublishWithDelay(topic string, msg []byte, _ time.Duration) error {
	return f.Publish(topic, msg)
}

func (f *liveDialerCallBillingFakePub) ValidateConnection() error { return nil }

func (f *liveDialerCallBillingFakePub) Count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.messages)
}

func (f *liveDialerCallBillingFakePub) Events(t *testing.T) []billing.CallCompletedEvent {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]billing.CallCompletedEvent, 0, len(f.messages))
	for _, raw := range f.messages {
		var ev billing.CallCompletedEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			t.Fatalf("unmarshal billing event: %v", err)
		}
		out = append(out, ev)
	}
	return out
}

func newBillingTestRunner(
	t *testing.T,
	adm dialer_domain.CallAdmissionCoordinator,
	pub *liveDialerCallBillingFakePub,
) *dialer_usecase.OutboundCallLifecycleRunner {
	t.Helper()
	return dialer_usecase.NewOutboundCallLifecycleRunner(adm, nil, nil, pub, log.New(testWriter{t}, "", 0))
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(string(p))
	return len(p), nil
}

func newLiveDialerCallForBilling(call conversation.CRMCall, lease *dialer_domain.CallAdmissionLease, startedAt time.Time) *liveDialerCall {
	return &liveDialerCall{
		call:          call,
		admission:     lease,
		workspaceID:   "ws-test",
		startedAt:     startedAt,
		lifecycleDone: make(chan struct{}),
	}
}

func TestLiveDialerCall_StartIsIdempotent_NoDoubleBilling(t *testing.T) {
	call := newFakeDialerCRMCall("call-idempotent")
	call.closeOnHangup = true
	lease := &dialer_domain.CallAdmissionLease{WorkspaceID: "ws-test", SlotAcquired: true}
	lc := newLiveDialerCallForBilling(call, lease, time.Now().Add(-5*time.Second))

	adm := &recordingAdmission{}
	pub := &liveDialerCallBillingFakePub{}
	runner := newBillingTestRunner(t, adm, pub)

	var onEndCalls atomic.Int32
	onEnd := func() { onEndCalls.Add(1) }

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); lc.start(context.Background(), runner, nil, log.Default(), onEnd) }()
	go func() { defer wg.Done(); lc.start(context.Background(), runner, nil, log.Default(), onEnd) }()
	wg.Wait()

	_ = call.Hangup()

	select {
	case <-lc.done():
	case <-time.After(2 * time.Second):
		t.Fatal("lifecycleDone never closed")
	}

	if got := onEndCalls.Load(); got != 1 {
		t.Fatalf("onEnd invocations = %d, want 1", got)
	}
	if got := adm.Releases(); got != 1 {
		t.Fatalf("admission.Release invocations = %d, want 1", got)
	}
	if got := pub.Count(); got != 1 {
		t.Fatalf("billing publishes = %d, want 1", got)
	}
}

func TestLiveDialerCall_LifecycleSurvivesForwarderSwap_OneBillingEvent(t *testing.T) {
	call := newFakeDialerCRMCall("call-transfer")
	call.closeOnHangup = true
	lease := &dialer_domain.CallAdmissionLease{WorkspaceID: "ws-test", SlotAcquired: true}
	startedAt := time.Now().Add(-30 * time.Second)
	lc := newLiveDialerCallForBilling(call, lease, startedAt)

	adm := &recordingAdmission{}
	pub := &liveDialerCallBillingFakePub{}
	runner := newBillingTestRunner(t, adm, pub)

	var onEndCalls atomic.Int32
	lc.start(context.Background(), runner, nil, log.Default(), func() { onEndCalls.Add(1) })

	lc.forwarder.Store(nil)
	lc.forwarder.Store(nil)

	call.events <- conversation.CallEvent{Type: "ringing"}
	lc.forwarder.Store(nil)
	call.events <- conversation.CallEvent{Type: "answered"}

	_ = call.Hangup()

	select {
	case <-lc.done():
	case <-time.After(2 * time.Second):
		t.Fatal("lifecycleDone never closed after Hangup")
	}

	if got := onEndCalls.Load(); got != 1 {
		t.Fatalf("onEnd invocations = %d, want exactly 1 across transfers", got)
	}
	if got := adm.Releases(); got != 1 {
		t.Fatalf("admission.Release invocations = %d, want exactly 1 across transfers", got)
	}
	evs := pub.Events(t)
	if len(evs) != 1 {
		t.Fatalf("billing events = %d, want exactly 1 across transfers", len(evs))
	}
	ev := evs[0]
	if ev.CallID != "call-transfer" || ev.WorkspaceID != "ws-test" {
		t.Fatalf("billing event identity mismatch: %+v", ev)
	}
	if ev.CallSource != billing.CallSourceWebSocket {
		t.Fatalf("CallSource = %q, want websocket", ev.CallSource)
	}
	if ev.DurationSec < 30 {
		t.Fatalf("DurationSec = %d, want >= 30 (full pre-Hangup duration, not just post-swap window)", ev.DurationSec)
	}
}

func TestLiveDialerCall_LifecycleDoneOrderingClosesLast(t *testing.T) {
	call := newFakeDialerCRMCall("call-ordering")
	call.closeOnHangup = true
	lease := &dialer_domain.CallAdmissionLease{WorkspaceID: "ws-test", SlotAcquired: true}
	lc := newLiveDialerCallForBilling(call, lease, time.Now().Add(-1*time.Second))

	adm := &recordingAdmission{}
	pub := &liveDialerCallBillingFakePub{}
	runner := newBillingTestRunner(t, adm, pub)

	releasedBeforeDone := atomic.Bool{}
	publishedBeforeDone := atomic.Bool{}
	onEndRan := atomic.Bool{}

	lc.start(context.Background(), runner, nil, log.Default(), func() {
		onEndRan.Store(true)
	})

	_ = call.Hangup()

	select {
	case <-lc.done():
	case <-time.After(2 * time.Second):
		t.Fatal("lifecycleDone never closed")
	}

	if adm.Releases() == 1 {
		releasedBeforeDone.Store(true)
	}
	if pub.Count() == 1 {
		publishedBeforeDone.Store(true)
	}

	if !onEndRan.Load() {
		t.Fatal("onEnd must have fired before lifecycleDone closed")
	}
	if !releasedBeforeDone.Load() {
		t.Fatalf("admission.Release must complete before lifecycleDone closes (got %d releases)", adm.Releases())
	}
	if !publishedBeforeDone.Load() {
		t.Fatalf("billing publish must complete before lifecycleDone closes (got %d publishes)", pub.Count())
	}
}

func TestLiveDialerCall_StartNilLifecycle_ReleasesAndExitsCleanly(t *testing.T) {
	call := newFakeDialerCRMCall("call-nil-lifecycle")
	lease := &dialer_domain.CallAdmissionLease{WorkspaceID: "ws-test", SlotAcquired: true}
	lc := newLiveDialerCallForBilling(call, lease, time.Now())

	adm := &recordingAdmission{}
	endUC := &recordingEndUseCase{admission: adm}

	onEndRan := atomic.Bool{}
	lc.start(context.Background(), nil, endUC, log.Default(), func() { onEndRan.Store(true) })

	select {
	case <-lc.done():
	case <-time.After(1 * time.Second):
		t.Fatal("lifecycleDone never closed on nil-lifecycle path")
	}
	if !onEndRan.Load() {
		t.Fatal("onEnd must fire even on the nil-lifecycle path")
	}

	if got := adm.Releases(); got != 1 {
		t.Fatalf("nil-lifecycle path released admission %d times, want 1", got)
	}
}

func TestLiveDialerCall_StartSurrendered_NoBillingNoAdmissionRelease(t *testing.T) {
	call := newFakeDialerCRMCall("call-surrendered")
	call.closeOnHangup = true

	lc := newLiveDialerCallForBilling(call, nil, time.Now().Add(-10*time.Second))

	onEndRan := atomic.Bool{}
	lc.startSurrendered(context.Background(), log.Default(), func() { onEndRan.Store(true) })

	_ = call.Hangup()

	select {
	case <-lc.done():
	case <-time.After(2 * time.Second):
		t.Fatal("lifecycleDone never closed on surrendered path")
	}
	if !onEndRan.Load() {
		t.Fatal("onEnd must fire on surrendered terminal exit (drives bridge onHumanHangup → publishCallCompleted)")
	}
}

func TestLiveDialerCall_StartSurrendered_OnEndFiresOnTerminalEvent(t *testing.T) {
	call := newFakeDialerCRMCall("call-surrendered-term")
	lc := newLiveDialerCallForBilling(call, nil, time.Now().Add(-7*time.Second))

	onEndCh := make(chan struct{})
	lc.startSurrendered(context.Background(), log.Default(), func() { close(onEndCh) })

	call.events <- conversation.CallEvent{Type: "ended", Reason: "peer_hangup"}

	select {
	case <-onEndCh:
	case <-time.After(2 * time.Second):
		t.Fatal("onEnd never fired on terminal event in surrendered lifecycle")
	}
	select {
	case <-lc.done():
	case <-time.After(2 * time.Second):
		t.Fatal("lifecycleDone never closed after terminal event")
	}
}

func TestLiveDialerCall_StartSurrendered_OnEndFiresOnContextCancel(t *testing.T) {
	call := newFakeDialerCRMCall("call-surrendered-ctx")
	lc := newLiveDialerCallForBilling(call, nil, time.Now())

	ctx, cancel := context.WithCancel(context.Background())
	onEndRan := atomic.Bool{}
	lc.startSurrendered(ctx, log.Default(), func() { onEndRan.Store(true) })

	cancel()

	select {
	case <-lc.done():
	case <-time.After(2 * time.Second):
		t.Fatal("lifecycleDone never closed on context cancel")
	}
	if !onEndRan.Load() {
		t.Fatal("onEnd must fire on ctx.Done in surrendered lifecycle")
	}
}

func TestLiveDialerCall_StartThenStartSurrendered_SecondIsNoOp(t *testing.T) {
	call := newFakeDialerCRMCall("call-cas-guard")
	call.closeOnHangup = true
	lease := &dialer_domain.CallAdmissionLease{WorkspaceID: "ws-test", SlotAcquired: true}
	lc := newLiveDialerCallForBilling(call, lease, time.Now().Add(-2*time.Second))

	adm := &recordingAdmission{}
	pub := &liveDialerCallBillingFakePub{}
	runner := newBillingTestRunner(t, adm, pub)

	var primaryOnEnd atomic.Int32
	var surrenderOnEnd atomic.Int32
	lc.start(context.Background(), runner, nil, log.Default(), func() { primaryOnEnd.Add(1) })

	lc.startSurrendered(context.Background(), log.Default(), func() { surrenderOnEnd.Add(1) })

	_ = call.Hangup()

	select {
	case <-lc.done():
	case <-time.After(2 * time.Second):
		t.Fatal("lifecycleDone never closed")
	}

	if got := primaryOnEnd.Load(); got != 1 {
		t.Fatalf("primary onEnd invocations = %d, want 1", got)
	}
	if got := surrenderOnEnd.Load(); got != 0 {
		t.Fatalf("surrender onEnd ran %d times on a call already owned by start() — CAS guard breached", got)
	}
	if got := pub.Count(); got != 1 {
		t.Fatalf("billing publishes = %d, want exactly 1 (no double-billing)", got)
	}
}

func TestLiveDialerCall_StartSurrenderedThenStart_SecondIsNoOp(t *testing.T) {
	call := newFakeDialerCRMCall("call-cas-guard-reverse")
	call.closeOnHangup = true
	lc := newLiveDialerCallForBilling(call, nil, time.Now())

	adm := &recordingAdmission{}
	pub := &liveDialerCallBillingFakePub{}
	runner := newBillingTestRunner(t, adm, pub)

	var surrenderOnEnd atomic.Int32
	var primaryOnEnd atomic.Int32
	lc.startSurrendered(context.Background(), log.Default(), func() { surrenderOnEnd.Add(1) })
	lc.start(context.Background(), runner, nil, log.Default(), func() { primaryOnEnd.Add(1) })

	_ = call.Hangup()

	select {
	case <-lc.done():
	case <-time.After(2 * time.Second):
		t.Fatal("lifecycleDone never closed")
	}

	if got := surrenderOnEnd.Load(); got != 1 {
		t.Fatalf("surrender onEnd invocations = %d, want 1", got)
	}
	if got := primaryOnEnd.Load(); got != 0 {
		t.Fatalf("primary onEnd ran %d times on a call already owned by startSurrendered() — CAS guard breached", got)
	}
	if got := pub.Count(); got != 0 {
		t.Fatalf("billing publishes = %d, want 0 (surrendered lifecycle owns the call; bridge publishes externally)", got)
	}
	if got := adm.Releases(); got != 0 {
		t.Fatalf("admission.Release ran %d times on surrendered-only call — must be 0", got)
	}
}
