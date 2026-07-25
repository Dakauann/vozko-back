package conversation_usecase

import (
	"context"
	"errors"
	"io"
	"log"
	"testing"
	"time"

	conversation_domain "vozko/domain/conversation"
	dialer "vozko/domain/dialer"
	dialer_usecase "vozko/usecases/dialer"
)

// waSession is a fake dialer.DialerSession for the WhatsApp inbound ring tests.
// Reserve/Release/HasActiveCall mirror the real session's token-scoped semantics
// so the tests exercise the reservation handoff exactly as production does. All
// mutation happens on the ringing goroutine; tests read s.reserved only after the
// goroutine has delivered its result on a channel (happens-before), so no lock is
// needed.
type waSession struct {
	id, userID, ws string
	reserved       string
	notifyErr      error
	notifyCh       chan dialer.DialerControlMessage
}

func newWASession(id, userID string) *waSession {
	return &waSession{id: id, userID: userID, ws: "ws-1", notifyCh: make(chan dialer.DialerControlMessage, 1)}
}

func (s *waSession) ID() string           { return s.id }
func (s *waSession) UserID() string       { return s.userID }
func (s *waSession) WorkspaceID() string  { return s.ws }
func (s *waSession) HasActiveCall() bool  { return s.reserved != "" }
func (s *waSession) ActiveCallID() string { return "" }
func (s *waSession) Reserve(token string) bool {
	if token == "" || s.reserved != "" {
		return false
	}
	s.reserved = token
	return true
}
func (s *waSession) Release(token string) {
	if s.reserved == token {
		s.reserved = ""
	}
}
func (s *waSession) Notify(msg dialer.DialerControlMessage) error {
	if s.notifyErr != nil {
		return s.notifyErr
	}
	if s.notifyCh != nil {
		s.notifyCh <- msg
	}
	return nil
}

func newRingUseCase() *WhatsAppInboundCallUseCase {
	return &WhatsAppInboundCallUseCase{
		broker: dialer_usecase.NewInboundOfferBroker(),
		log:    log.New(io.Discard, "", 0),
	}
}

func waOffer(offerID string, cand *waSession, ring time.Duration) dialer.InboundCallOffer {
	return dialer.InboundCallOffer{
		OfferID:     offerID,
		CallID:      "wa-call-1",
		WorkspaceID: cand.ws,
		FromNumber:  "+5511999999999",
		ToNumber:    "+5511888888888",
		Channel:     "whatsapp",
		ExpiresAt:   time.Now().Add(ring),
	}
}

func acceptOffer(t *testing.T, uc *WhatsAppInboundCallUseCase, offer dialer.InboundCallOffer, cand *waSession) {
	t.Helper()
	if err := uc.broker.Accept(context.Background(), dialer.AcceptInboundCallInput{
		OfferID: offer.OfferID, WorkspaceID: offer.WorkspaceID, UserID: cand.userID, SessionID: cand.id,
	}); err != nil {
		t.Fatalf("broker.Accept: %v", err)
	}
}

func declineOffer(t *testing.T, uc *WhatsAppInboundCallUseCase, offer dialer.InboundCallOffer, cand *waSession) {
	t.Helper()
	if err := uc.broker.Decline(context.Background(), dialer.DeclineInboundCallInput{
		OfferID: offer.OfferID, WorkspaceID: offer.WorkspaceID, UserID: cand.userID, SessionID: cand.id, Reason: "busy",
	}); err != nil {
		t.Fatalf("broker.Decline: %v", err)
	}
}

func TestRingCandidate_AcceptHoldsReservation(t *testing.T) {
	uc := newRingUseCase()
	cand := newWASession("s-1", "u-1")
	offer := waOffer("offer-1", cand, 2*time.Second)
	signals := make(chan conversation_domain.WhatsAppCallSignal)

	res := make(chan candidateOutcome, 1)
	go func() { res <- uc.ringCandidate(context.Background(), cand, offer, signals, 2*time.Second, nil) }()

	<-cand.notifyCh // offer rang
	acceptOffer(t, uc, offer, cand)

	if got := <-res; got != candAccepted {
		t.Fatalf("outcome = %v, want candAccepted", got)
	}
	if cand.reserved != "offer-1" {
		t.Fatalf("accept must keep the reservation held for the attach handoff, got %q", cand.reserved)
	}
}

func TestRingCandidate_DeclineReleasesReservation(t *testing.T) {
	uc := newRingUseCase()
	cand := newWASession("s-1", "u-1")
	offer := waOffer("offer-1", cand, 2*time.Second)
	signals := make(chan conversation_domain.WhatsAppCallSignal)

	res := make(chan candidateOutcome, 1)
	go func() { res <- uc.ringCandidate(context.Background(), cand, offer, signals, 2*time.Second, nil) }()

	<-cand.notifyCh
	declineOffer(t, uc, offer, cand)

	if got := <-res; got != candDeclined {
		t.Fatalf("outcome = %v, want candDeclined", got)
	}
	if cand.reserved != "" {
		t.Fatalf("decline must release the reservation, got %q", cand.reserved)
	}
}

func TestRingCandidate_TimeoutReleasesReservation(t *testing.T) {
	uc := newRingUseCase()
	cand := newWASession("s-1", "u-1")
	offer := waOffer("offer-1", cand, 30*time.Millisecond)
	signals := make(chan conversation_domain.WhatsAppCallSignal)

	res := make(chan candidateOutcome, 1)
	go func() { res <- uc.ringCandidate(context.Background(), cand, offer, signals, 30*time.Millisecond, nil) }()

	if got := <-res; got != candTimedOut {
		t.Fatalf("outcome = %v, want candTimedOut", got)
	}
	if cand.reserved != "" {
		t.Fatalf("no-answer timeout must release the reservation, got %q", cand.reserved)
	}
}

func TestRingCandidate_ContextCancelReleasesReservation(t *testing.T) {
	uc := newRingUseCase()
	cand := newWASession("s-1", "u-1")
	offer := waOffer("offer-1", cand, 2*time.Second)
	signals := make(chan conversation_domain.WhatsAppCallSignal)

	ctx, cancel := context.WithCancel(context.Background())
	res := make(chan candidateOutcome, 1)
	go func() { res <- uc.ringCandidate(ctx, cand, offer, signals, 2*time.Second, nil) }()

	<-cand.notifyCh
	cancel()

	if got := <-res; got != candTerminated {
		t.Fatalf("outcome = %v, want candTerminated", got)
	}
	if cand.reserved != "" {
		t.Fatalf("context cancel must release the reservation, got %q", cand.reserved)
	}
}

func TestRingCandidate_CallerHangupReleasesReservation(t *testing.T) {
	uc := newRingUseCase()
	cand := newWASession("s-1", "u-1")
	offer := waOffer("offer-1", cand, 2*time.Second)
	signals := make(chan conversation_domain.WhatsAppCallSignal, 1)

	res := make(chan candidateOutcome, 1)
	go func() { res <- uc.ringCandidate(context.Background(), cand, offer, signals, 2*time.Second, nil) }()

	<-cand.notifyCh
	signals <- conversation_domain.WhatsAppCallSignal{Kind: conversation_domain.WhatsAppCallTerminate}

	if got := <-res; got != candTerminated {
		t.Fatalf("outcome = %v, want candTerminated", got)
	}
	if cand.reserved != "" {
		t.Fatalf("caller hangup must release the reservation, got %q", cand.reserved)
	}
}

func TestRingCandidate_ReserveFailsLeavesForeignReservationUntouched(t *testing.T) {
	uc := newRingUseCase()
	cand := newWASession("s-1", "u-1")
	cand.reserved = "foreign" // another flow already rang this agent
	offer := waOffer("offer-1", cand, time.Second)
	signals := make(chan conversation_domain.WhatsAppCallSignal)

	got := uc.ringCandidate(context.Background(), cand, offer, signals, time.Second, nil)
	if got != candUnavailable {
		t.Fatalf("outcome = %v, want candUnavailable", got)
	}
	if cand.reserved != "foreign" {
		t.Fatalf("a failed reserve must not touch the existing reservation, got %q", cand.reserved)
	}
	select {
	case <-cand.notifyCh:
		t.Fatal("an agent we could not reserve must not be rung")
	default:
	}
}

func TestRingCandidate_NotifyFailureReleasesReservation(t *testing.T) {
	uc := newRingUseCase()
	cand := newWASession("s-1", "u-1")
	cand.notifyErr = errors.New("ws closed")
	offer := waOffer("offer-1", cand, time.Second)
	signals := make(chan conversation_domain.WhatsAppCallSignal)

	got := uc.ringCandidate(context.Background(), cand, offer, signals, time.Second, nil)
	if got != candUnavailable {
		t.Fatalf("outcome = %v, want candUnavailable", got)
	}
	if cand.reserved != "" {
		t.Fatalf("a failed ring must release the reservation it took, got %q", cand.reserved)
	}
}

func TestRingCandidate_OnReservedRunsAfterReserveBeforeRing(t *testing.T) {
	uc := newRingUseCase()
	cand := newWASession("s-1", "u-1")
	offer := waOffer("offer-1", cand, 2*time.Second)
	signals := make(chan conversation_domain.WhatsAppCallSignal)

	reservedAtCallback := ""
	onReserved := func() { reservedAtCallback = cand.reserved }

	res := make(chan candidateOutcome, 1)
	go func() { res <- uc.ringCandidate(context.Background(), cand, offer, signals, 2*time.Second, onReserved) }()
	<-cand.notifyCh
	acceptOffer(t, uc, offer, cand)
	<-res

	if reservedAtCallback != "offer-1" {
		t.Fatalf("onReserved must run with the reservation already held, saw %q", reservedAtCallback)
	}
}

// ringSequentially must fall through a declining agent to the next, releasing the
// decliner so they are free again, and return the accepting agent with its
// reservation still held for the attach handoff.
func TestRingSequentially_FallsThroughDeclineToAccept(t *testing.T) {
	uc := newRingUseCase()
	c1 := newWASession("s-1", "u-1")
	c2 := newWASession("s-2", "u-2")
	connect := conversation_domain.WhatsAppInboundConnect{CallID: "wa-call-1", FromNumber: "+5511999999999", ToNumber: "+5511888888888"}
	signals := make(chan conversation_domain.WhatsAppCallSignal)
	deadline := time.Now().Add(5 * time.Second)

	resCh := make(chan *ringOutcome, 1)
	go func() {
		resCh <- uc.ringSequentially(context.Background(), connect, "bp-1", "ws-1", "", false,
			[]dialer.DialerSession{c1, c2}, signals, deadline)
	}()

	// First agent rings and declines.
	msg1 := <-c1.notifyCh
	offer1 := msg1.Payload.(dialer.InboundCallOffer)
	declineOffer(t, uc, offer1, c1)

	// Roulette falls through to the second agent, who accepts.
	msg2 := <-c2.notifyCh
	offer2 := msg2.Payload.(dialer.InboundCallOffer)
	acceptOffer(t, uc, offer2, c2)

	outcome := <-resCh
	if outcome.session != dialer.DialerSession(c2) {
		t.Fatalf("expected the accepting agent (c2) to be returned, got %v", outcome.session)
	}
	if outcome.offerID != offer2.OfferID {
		t.Fatalf("returned offerID = %q, want %q", outcome.offerID, offer2.OfferID)
	}
	if c1.reserved != "" {
		t.Fatalf("the declining agent must be released, still %q", c1.reserved)
	}
	if c2.reserved != offer2.OfferID {
		t.Fatalf("the accepting agent must stay reserved for the attach handoff, got %q", c2.reserved)
	}
}

// When every candidate lets the ring time out, ringSequentially reports no
// session and leaves nobody reserved.
func TestRingSequentially_AllTimeoutLeavesNobodyReserved(t *testing.T) {
	uc := newRingUseCase()
	c1 := newWASession("s-1", "u-1")
	connect := conversation_domain.WhatsAppInboundConnect{CallID: "wa-call-1", FromNumber: "+5511999999999"}
	signals := make(chan conversation_domain.WhatsAppCallSignal)
	// Deadline far enough to ring once, but the per-candidate ring is bounded by it.
	deadline := time.Now().Add(40 * time.Millisecond)

	outcome := uc.ringSequentially(context.Background(), connect, "bp-1", "ws-1", "", false,
		[]dialer.DialerSession{c1}, signals, deadline)

	if outcome.session != nil {
		t.Fatalf("no acceptance should yield a nil session, got %v", outcome.session)
	}
	if c1.reserved != "" {
		t.Fatalf("a timed-out candidate must be released, still %q", c1.reserved)
	}
}
