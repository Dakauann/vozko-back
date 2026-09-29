package callsession_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	callsession "vozko/domain/callsession"
)

type ringSession struct {
	id, userID, ws string
	reserved       string
	notifyErr      error
	notifyCh       chan callsession.CallSessionControlMessage
}

func newRingSession(id, userID string) *ringSession {
	return &ringSession{id: id, userID: userID, ws: "ws-1", notifyCh: make(chan callsession.CallSessionControlMessage, 4)}
}

func (s *ringSession) ID() string           { return s.id }
func (s *ringSession) UserID() string       { return s.userID }
func (s *ringSession) WorkspaceID() string  { return s.ws }
func (s *ringSession) HasActiveCall() bool  { return s.reserved != "" }
func (s *ringSession) ActiveCallID() string { return "" }
func (s *ringSession) Reserve(token string) bool {
	if token == "" || s.reserved != "" {
		return false
	}
	s.reserved = token
	return true
}
func (s *ringSession) Release(token string) {
	if s.reserved == token {
		s.reserved = ""
	}
}
func (s *ringSession) Notify(msg callsession.CallSessionControlMessage) error {
	if s.notifyErr != nil {
		return s.notifyErr
	}
	if s.notifyCh != nil {
		s.notifyCh <- msg
	}
	return nil
}

func newTestRinger() *InboundRinger {
	return NewInboundRinger(NewInboundOfferBroker())
}

func waOffer(offerID string, cand *ringSession, ring time.Duration) callsession.InboundCallOffer {
	return callsession.InboundCallOffer{
		OfferID:     offerID,
		CallID:      "wa-call-1",
		WorkspaceID: cand.ws,
		FromNumber:  "+5511999999999",
		ToNumber:    "+5511888888888",
		Channel:     "whatsapp",
		ExpiresAt:   time.Now().Add(ring),
	}
}

func acceptOffer(t *testing.T, uc *InboundRinger, offer callsession.InboundCallOffer, cand *ringSession) {
	t.Helper()
	if err := uc.broker.Accept(context.Background(), callsession.AcceptInboundCallInput{
		OfferID: offer.OfferID, WorkspaceID: offer.WorkspaceID, UserID: cand.userID, SessionID: cand.id,
	}); err != nil {
		t.Fatalf("broker.Accept: %v", err)
	}
}

func declineOffer(t *testing.T, uc *InboundRinger, offer callsession.InboundCallOffer, cand *ringSession) {
	t.Helper()
	if err := uc.broker.Decline(context.Background(), callsession.DeclineInboundCallInput{
		OfferID: offer.OfferID, WorkspaceID: offer.WorkspaceID, UserID: cand.userID, SessionID: cand.id, Reason: "busy",
	}); err != nil {
		t.Fatalf("broker.Decline: %v", err)
	}
}

func TestRingCandidate_AcceptHoldsReservation(t *testing.T) {
	uc := newTestRinger()
	cand := newRingSession("s-1", "u-1")
	offer := waOffer("offer-1", cand, 2*time.Second)
	callerGone := make(chan struct{})

	res := make(chan candidateOutcome, 1)
	go func() { res <- uc.ringCandidate(context.Background(), cand, offer, callerGone, 2*time.Second, nil) }()

	<-cand.notifyCh
	acceptOffer(t, uc, offer, cand)

	if got := <-res; got != candAccepted {
		t.Fatalf("outcome = %v, want candAccepted", got)
	}
	if cand.reserved != "offer-1" {
		t.Fatalf("accept must keep the reservation held for the attach handoff, got %q", cand.reserved)
	}
}

func TestRingCandidate_DeclineReleasesReservation(t *testing.T) {
	uc := newTestRinger()
	cand := newRingSession("s-1", "u-1")
	offer := waOffer("offer-1", cand, 2*time.Second)
	callerGone := make(chan struct{})

	res := make(chan candidateOutcome, 1)
	go func() { res <- uc.ringCandidate(context.Background(), cand, offer, callerGone, 2*time.Second, nil) }()

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
	uc := newTestRinger()
	cand := newRingSession("s-1", "u-1")
	offer := waOffer("offer-1", cand, 30*time.Millisecond)
	callerGone := make(chan struct{})

	res := make(chan candidateOutcome, 1)
	go func() {
		res <- uc.ringCandidate(context.Background(), cand, offer, callerGone, 30*time.Millisecond, nil)
	}()

	if got := <-res; got != candTimedOut {
		t.Fatalf("outcome = %v, want candTimedOut", got)
	}
	if cand.reserved != "" {
		t.Fatalf("no-answer timeout must release the reservation, got %q", cand.reserved)
	}
}

func TestRingCandidate_ContextCancelReleasesReservation(t *testing.T) {
	uc := newTestRinger()
	cand := newRingSession("s-1", "u-1")
	offer := waOffer("offer-1", cand, 2*time.Second)
	callerGone := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	res := make(chan candidateOutcome, 1)
	go func() { res <- uc.ringCandidate(ctx, cand, offer, callerGone, 2*time.Second, nil) }()

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
	uc := newTestRinger()
	cand := newRingSession("s-1", "u-1")
	offer := waOffer("offer-1", cand, 2*time.Second)
	callerGone := make(chan struct{})

	res := make(chan candidateOutcome, 1)
	go func() { res <- uc.ringCandidate(context.Background(), cand, offer, callerGone, 2*time.Second, nil) }()

	<-cand.notifyCh
	close(callerGone)

	if got := <-res; got != candTerminated {
		t.Fatalf("outcome = %v, want candTerminated", got)
	}
	if cand.reserved != "" {
		t.Fatalf("caller hangup must release the reservation, got %q", cand.reserved)
	}
}

func TestRingCandidate_ReserveFailsLeavesForeignReservationUntouched(t *testing.T) {
	uc := newTestRinger()
	cand := newRingSession("s-1", "u-1")
	cand.reserved = "foreign"
	offer := waOffer("offer-1", cand, time.Second)
	callerGone := make(chan struct{})

	got := uc.ringCandidate(context.Background(), cand, offer, callerGone, time.Second, nil)
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
	uc := newTestRinger()
	cand := newRingSession("s-1", "u-1")
	cand.notifyErr = errors.New("ws closed")
	offer := waOffer("offer-1", cand, time.Second)
	callerGone := make(chan struct{})

	got := uc.ringCandidate(context.Background(), cand, offer, callerGone, time.Second, nil)
	if got != candUnavailable {
		t.Fatalf("outcome = %v, want candUnavailable", got)
	}
	if cand.reserved != "" {
		t.Fatalf("a failed ring must release the reservation it took, got %q", cand.reserved)
	}
}

func TestRingCandidate_OnReservedRunsAfterReserveBeforeRing(t *testing.T) {
	uc := newTestRinger()
	cand := newRingSession("s-1", "u-1")
	offer := waOffer("offer-1", cand, 2*time.Second)
	callerGone := make(chan struct{})

	reservedAtCallback := ""
	onReserved := func() { reservedAtCallback = cand.reserved }

	res := make(chan candidateOutcome, 1)
	go func() {
		res <- uc.ringCandidate(context.Background(), cand, offer, callerGone, 2*time.Second, onReserved)
	}()
	<-cand.notifyCh
	acceptOffer(t, uc, offer, cand)
	<-res

	if reservedAtCallback != "offer-1" {
		t.Fatalf("onReserved must run with the reservation already held, saw %q", reservedAtCallback)
	}
}

func ringRequest(deadline time.Time, callerGone <-chan struct{}, candidates ...callsession.CallSession) RingRequest {
	return RingRequest{
		Offer:        callsession.InboundCallOffer{CallID: "call-1", WorkspaceID: "ws-1", FromNumber: "+5511999999999", Channel: "sip"},
		Candidates:   candidates,
		PerCandidate: 15 * time.Second,
		Deadline:     deadline,
		CallerGone:   callerGone,
	}
}

func TestRingFallsThroughADeclineToTheNextMember(t *testing.T) {
	uc := newTestRinger()
	c1 := newRingSession("s-1", "u-1")
	c2 := newRingSession("s-2", "u-2")

	resCh := make(chan RingOutcome, 1)
	go func() {
		resCh <- uc.Ring(context.Background(), ringRequest(time.Now().Add(5*time.Second), make(chan struct{}), c1, c2))
	}()

	offer1 := (<-c1.notifyCh).Payload.(callsession.InboundCallOffer)
	declineOffer(t, uc, offer1, c1)
	offer2 := (<-c2.notifyCh).Payload.(callsession.InboundCallOffer)
	acceptOffer(t, uc, offer2, c2)

	outcome := <-resCh
	if outcome.Session != callsession.CallSession(c2) || outcome.OfferID != offer2.OfferID {
		t.Fatalf("outcome = %+v, want the accepting member with its offer", outcome)
	}
	if offer1.OfferID == offer2.OfferID || offer1.Channel != "sip" || offer2.CallID != "call-1" {
		t.Fatalf("offers %+v and %+v must be distinct and carry the call template", offer1, offer2)
	}
	if c1.reserved != "" || c2.reserved != offer2.OfferID {
		t.Fatalf("reservations c1=%q c2=%q, want only the accepting member held", c1.reserved, c2.reserved)
	}
}

func TestRingThatNobodyAnswersLeavesNobodyReserved(t *testing.T) {
	uc := newTestRinger()
	c1 := newRingSession("s-1", "u-1")
	outcome := uc.Ring(context.Background(), ringRequest(time.Now().Add(40*time.Millisecond), make(chan struct{}), c1))
	if outcome.Session != nil || outcome.CallerGone {
		t.Fatalf("outcome = %+v, want nobody and no hangup", outcome)
	}
	if c1.reserved != "" {
		t.Fatalf("a timed-out member must be released, still %q", c1.reserved)
	}
}

func TestRingStopsWhenTheCallerHangsUp(t *testing.T) {
	uc := newTestRinger()
	c1 := newRingSession("s-1", "u-1")
	c2 := newRingSession("s-2", "u-2")
	callerGone := make(chan struct{})

	resCh := make(chan RingOutcome, 1)
	go func() {
		resCh <- uc.Ring(context.Background(), ringRequest(time.Now().Add(5*time.Second), callerGone, c1, c2))
	}()
	<-c1.notifyCh
	close(callerGone)

	outcome := <-resCh
	if !outcome.CallerGone || outcome.Session != nil {
		t.Fatalf("outcome = %+v, want CallerGone", outcome)
	}
	if c1.reserved != "" {
		t.Fatalf("the rung member must be released, still %q", c1.reserved)
	}
	select {
	case <-c2.notifyCh:
		t.Fatal("a member was rung after the caller hung up")
	default:
	}
}

func TestRingSkipsBusyMembersAndHandsTheReservedMemberToOnReserved(t *testing.T) {
	uc := newTestRinger()
	busy := newRingSession("s-1", "u-1")
	busy.reserved = "another-call"
	free := newRingSession("s-2", "u-2")
	var reservedFor callsession.CallSession
	req := ringRequest(time.Now().Add(5*time.Second), make(chan struct{}), busy, free)
	req.OnReserved = func(s callsession.CallSession) { reservedFor = s }

	resCh := make(chan RingOutcome, 1)
	go func() { resCh <- uc.Ring(context.Background(), req) }()
	offer := (<-free.notifyCh).Payload.(callsession.InboundCallOffer)
	acceptOffer(t, uc, offer, free)
	<-resCh

	if reservedFor != callsession.CallSession(free) {
		t.Fatalf("OnReserved got %v, want the free member", reservedFor)
	}
	select {
	case <-busy.notifyCh:
		t.Fatal("a member already on a call was rung")
	default:
	}
}

func withdrawnOffer(t *testing.T, member *ringSession) callsession.InboundOfferWithdrawn {
	t.Helper()
	select {
	case msg := <-member.notifyCh:
		if msg.Type != callsession.CallSessionInboundCallWithdrawn {
			t.Fatalf("message = %s, want the offer withdrawn", msg.Type)
		}
		return msg.Payload.(callsession.InboundOfferWithdrawn)
	case <-time.After(time.Second):
		t.Fatal("the member was never told the offer is gone")
	}
	return callsession.InboundOfferWithdrawn{}
}

func TestAMemberIsToldWhenTheirOfferExpires(t *testing.T) {
	uc := newTestRinger()
	member := newRingSession("s-1", "u-1")
	uc.Ring(context.Background(), ringRequest(time.Now().Add(40*time.Millisecond), make(chan struct{}), member))
	offer := (<-member.notifyCh).Payload.(callsession.InboundCallOffer)
	if withdrawn := withdrawnOffer(t, member); withdrawn.OfferID != offer.OfferID || withdrawn.Reason != "no_answer" {
		t.Fatalf("withdrawn = %+v, want offer %s withdrawn for no_answer", withdrawn, offer.OfferID)
	}
}

func TestAMemberIsToldWhenTheCallerHangsUpWhileRinging(t *testing.T) {
	uc := newTestRinger()
	member := newRingSession("s-1", "u-1")
	callerGone := make(chan struct{})
	done := make(chan struct{})
	go func() {
		uc.Ring(context.Background(), ringRequest(time.Now().Add(5*time.Second), callerGone, member))
		close(done)
	}()
	offer := (<-member.notifyCh).Payload.(callsession.InboundCallOffer)
	close(callerGone)
	<-done
	if withdrawn := withdrawnOffer(t, member); withdrawn.OfferID != offer.OfferID || withdrawn.Reason != "caller_hung_up" {
		t.Fatalf("withdrawn = %+v, want caller_hung_up", withdrawn)
	}
}

func TestAnAcceptedOfferIsNotWithdrawn(t *testing.T) {
	uc := newTestRinger()
	member := newRingSession("s-1", "u-1")
	resCh := make(chan RingOutcome, 1)
	go func() {
		resCh <- uc.Ring(context.Background(), ringRequest(time.Now().Add(5*time.Second), make(chan struct{}), member))
	}()
	offer := (<-member.notifyCh).Payload.(callsession.InboundCallOffer)
	acceptOffer(t, uc, offer, member)
	<-resCh
	select {
	case msg := <-member.notifyCh:
		t.Fatalf("an accepted offer sent %s", msg.Type)
	default:
	}
}
