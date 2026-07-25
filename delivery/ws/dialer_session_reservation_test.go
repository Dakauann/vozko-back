package ws

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vozko/domain/conversation"
)

// reservationTestCall is a minimal conversation.CRMCall used only to give a
// session an attached call so we can exercise the reserved-vs-attached boundary.
type reservationTestCall struct {
	id   string
	done chan struct{}
}

func newReservationTestCall(id string) *reservationTestCall {
	return &reservationTestCall{id: id, done: make(chan struct{})}
}

func (c *reservationTestCall) ID() string                 { return c.id }
func (c *reservationTestCall) SendAudio([]byte) error     { return nil }
func (c *reservationTestCall) AudioStream() <-chan []byte { return make(chan []byte) }
func (c *reservationTestCall) Events() <-chan conversation.CallEvent {
	return make(chan conversation.CallEvent)
}
func (c *reservationTestCall) Hangup() error         { return nil }
func (c *reservationTestCall) Done() <-chan struct{} { return c.done }

func newReservationTestSession() *dialerSession {
	return newDialerSession("sess-1", "user-1", "ws-1", func(*WSOutgoingMessage) {}, nil, nil, 0)
}

func attachTestCall(t *testing.T, s *dialerSession, callID string) *liveDialerCall {
	t.Helper()
	lc := &liveDialerCall{
		call:          newReservationTestCall(callID),
		lifecycleDone: make(chan struct{}),
	}
	if err := s.Attach(lc); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	return lc
}

func TestReserve_OnIdleSessionSucceeds(t *testing.T) {
	s := newReservationTestSession()
	if !s.Reserve("offer-1") {
		t.Fatal("Reserve on idle session should succeed")
	}
	if !s.HasActiveCall() {
		t.Fatal("a reserved session must report HasActiveCall()=true so it is excluded from routing")
	}
	if s.ActiveCallID() != "" {
		t.Fatalf("ActiveCallID must be empty while only reserved, got %q", s.ActiveCallID())
	}
}

func TestReserve_EmptyTokenRejected(t *testing.T) {
	s := newReservationTestSession()
	if s.Reserve("") {
		t.Fatal("Reserve(\"\") must fail")
	}
	if s.HasActiveCall() {
		t.Fatal("empty-token reserve must not occupy the session")
	}
}

func TestReserve_IdempotentForSameToken(t *testing.T) {
	s := newReservationTestSession()
	if !s.Reserve("offer-1") {
		t.Fatal("first Reserve should succeed")
	}
	if !s.Reserve("offer-1") {
		t.Fatal("re-reserving with the same token must be idempotent (true)")
	}
}

func TestReserve_DifferentTokenRejectedWhileReserved(t *testing.T) {
	s := newReservationTestSession()
	if !s.Reserve("offer-1") {
		t.Fatal("first Reserve should succeed")
	}
	if s.Reserve("offer-2") {
		t.Fatal("a second concurrent offer must NOT be able to reserve an already-ringing agent")
	}
}

func TestReserve_RejectedWhenCallAttached(t *testing.T) {
	s := newReservationTestSession()
	attachTestCall(t, s, "call-1")
	if s.Reserve("offer-1") {
		t.Fatal("must not reserve a session that already has an attached call")
	}
	if s.ActiveCallID() != "call-1" {
		t.Fatalf("ActiveCallID = %q, want call-1", s.ActiveCallID())
	}
}

func TestRelease_TokenScoped(t *testing.T) {
	s := newReservationTestSession()
	s.Reserve("offer-1")

	s.Release("offer-2") // foreign token: must be a no-op
	if !s.HasActiveCall() {
		t.Fatal("releasing a foreign token must not clear the reservation")
	}

	s.Release("offer-1")
	if s.HasActiveCall() {
		t.Fatal("releasing the matching token must clear the reservation")
	}
}

func TestRelease_IdempotentAndEmptyTokenSafe(t *testing.T) {
	s := newReservationTestSession()
	s.Reserve("offer-1")
	s.Release("") // no-op, must not clear
	if !s.HasActiveCall() {
		t.Fatal("Release(\"\") must not clear a live reservation")
	}
	s.Release("offer-1")
	s.Release("offer-1") // double release must be safe
	if s.HasActiveCall() {
		t.Fatal("session should be free after release")
	}
	// Re-reserve must work after release.
	if !s.Reserve("offer-2") {
		t.Fatal("Reserve after Release should succeed")
	}
}

func TestReserve_AfterAnotherReleasedTokenSucceeds(t *testing.T) {
	s := newReservationTestSession()
	s.Reserve("offer-1")
	s.Release("offer-1")
	if !s.Reserve("offer-2") {
		t.Fatal("a freed session must be reservable by a new offer")
	}
}

func TestAttach_ConsumesReservation(t *testing.T) {
	s := newReservationTestSession()
	if !s.Reserve("offer-1") {
		t.Fatal("Reserve should succeed")
	}
	attachTestCall(t, s, "call-1")

	// Accept consumed the reservation: after the call detaches, the session must be
	// fully free. If Attach had NOT cleared `reserved`, HasActiveCall would still be
	// true here and a stale reservation would leak.
	if _, ok := s.Detach(); !ok {
		t.Fatal("Detach should report the attached call")
	}
	if s.HasActiveCall() {
		t.Fatal("session must be free after detach; Attach must have consumed the reservation")
	}
	// A stale Release for the consumed token must not resurrect/confuse state.
	s.Release("offer-1")
	if s.HasActiveCall() {
		t.Fatal("session must remain free")
	}
}

func TestAttach_FailsWhenAlreadyAttached(t *testing.T) {
	s := newReservationTestSession()
	attachTestCall(t, s, "call-1")
	lc2 := &liveDialerCall{call: newReservationTestCall("call-2"), lifecycleDone: make(chan struct{})}
	if err := s.Attach(lc2); err != errDialerSessionBusy {
		t.Fatalf("second Attach err = %v, want errDialerSessionBusy", err)
	}
}

func TestReserve_TTLBackstopLazilyExpires(t *testing.T) {
	s := newReservationTestSession()
	base := time.Unix(1_700_000_000, 0)
	cur := base
	s.now = func() time.Time { return cur }

	if !s.Reserve("offer-1") {
		t.Fatal("Reserve should succeed")
	}
	// Still within TTL: occupied.
	cur = base.Add(dialerReservationTTL - time.Millisecond)
	if !s.HasActiveCall() {
		t.Fatal("reservation within TTL must still be occupied")
	}
	// Past TTL: the backstop lazily frees it even though Release was never called.
	cur = base.Add(dialerReservationTTL)
	if s.HasActiveCall() {
		t.Fatal("reservation past TTL must be treated as expired (leak backstop)")
	}
	// And the slot is reusable by a fresh offer.
	if !s.Reserve("offer-2") {
		t.Fatal("a fresh offer must be able to reserve after TTL expiry")
	}
}

func TestShutdown_ReleasesRingReservation(t *testing.T) {
	s := newReservationTestSession()
	if !s.Reserve("offer-1") {
		t.Fatal("Reserve should succeed")
	}
	// No call attached: an agent disconnecting mid-ring. Shutdown must still free
	// the reservation (otherwise it leaks until the TTL backstop).
	s.Shutdown(context.Background())
	if s.HasActiveCall() {
		t.Fatal("Shutdown must release an outstanding ring reservation")
	}
}

// Regression: a natural call end (far side hung up -> dispatchEnded) must broadcast
// a presence change, otherwise OTHER members keep seeing this agent as busy in the
// roster/transfer picker even though they ended their call.
func TestDispatchEnded_ClearsCurrentAndBroadcastsPresence(t *testing.T) {
	s := newReservationTestSession()
	var presenceCalls int32
	s.SetPresenceCallback(func() { atomic.AddInt32(&presenceCalls, 1) })

	lc := attachTestCall(t, s, "call-1")
	// Attach broadcast once; isolate the call-end broadcast.
	atomic.StoreInt32(&presenceCalls, 0)

	s.dispatchEnded(lc, "ended", 0)

	if s.HasActiveCall() {
		t.Fatal("dispatchEnded must clear the current call so the agent is free")
	}
	if got := atomic.LoadInt32(&presenceCalls); got != 1 {
		t.Fatalf("a natural call end must broadcast presence exactly once, got %d", got)
	}

	// A stale/foreign leg's dispatchEnded (not the current call) must NOT broadcast.
	other := &liveDialerCall{call: newReservationTestCall("call-2"), lifecycleDone: make(chan struct{})}
	atomic.StoreInt32(&presenceCalls, 0)
	s.dispatchEnded(other, "ended", 0)
	if got := atomic.LoadInt32(&presenceCalls); got != 0 {
		t.Fatalf("ending a non-current leg must not broadcast presence, got %d", got)
	}
}

func TestReserve_ConcurrentOnlyOneWinner(t *testing.T) {
	s := newReservationTestSession()
	const n = 64
	var wins int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(n)
	for i := 0; i < n; i++ {
		token := "offer-" + itoaReservation(i)
		go func() {
			defer wg.Done()
			<-start
			if s.Reserve(token) {
				atomic.AddInt32(&wins, 1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if wins != 1 {
		t.Fatalf("exactly one concurrent Reserve must win, got %d winners", wins)
	}
	if !s.HasActiveCall() {
		t.Fatal("session must be reserved after the race")
	}
}

func itoaReservation(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
