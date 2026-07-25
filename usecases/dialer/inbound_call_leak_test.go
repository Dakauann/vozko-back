package dialer_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/dialer"
)

// These tests reproduce the reported symptom (an agent flooded with inbound calls
// declines a ring but is then told "you already have an active call" when dialing
// out) and lock in the fix: the ring RESERVATION that marks the agent occupied
// must be released on every decline path, including when the dialer reconnected
// with a fresh session id.

// A well-formed decline (matching offer/ws/user/session) frees the reservation
// immediately, so the agent can dial out right after rejecting.
func TestInboundDecline_ReleasesReservation_NoLeak(t *testing.T) {
	session := newInboundTestSession("s-1", "u-1", "ws-1")
	uc := newInboundTestUseCase(t, &inboundTestRegistry{available: []dialer.DialerSession{session}}, &inboundTestAdmission{}, nil)
	done := runInboundInvite(t, uc, &inboundTestDialog{id: "dlg-1", from: "+5511999999999", to: "1000"})

	offer := waitInboundOffer(t, session).Payload.(dialer.InboundCallOffer)
	if !session.HasActiveCall() {
		t.Fatal("session should be reserved while the inbound offer is ringing")
	}

	if err := uc.Decline(context.Background(), dialer.DeclineInboundCallInput{
		OfferID: offer.OfferID, WorkspaceID: "ws-1", UserID: "u-1", SessionID: "s-1",
	}); err != nil {
		t.Fatalf("Decline: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("HandleInboundInvite returned %v", err)
	}
	if session.HasActiveCall() {
		t.Fatal("LEAK: session still reserved after a valid decline")
	}
}

// THE FIX: the dialer WS dropped and reconnected with a new session id (seen in
// the production logs), so the decline carries a different session id than the
// offer's target. It must still cancel the ring (user-level match) and free the
// agent immediately — not leave it stuck until the offer TTL (~30s).
func TestInboundDecline_FromReconnectedSession_StillCancels(t *testing.T) {
	session := newInboundTestSession("s-1", "u-1", "ws-1")
	uc := newInboundTestUseCase(t, &inboundTestRegistry{available: []dialer.DialerSession{session}}, &inboundTestAdmission{}, nil)
	done := runInboundInvite(t, uc, &inboundTestDialog{id: "dlg-1", from: "+5511999999999", to: "1000"})

	offer := waitInboundOffer(t, session).Payload.(dialer.InboundCallOffer)

	if err := uc.Decline(context.Background(), dialer.DeclineInboundCallInput{
		OfferID: offer.OfferID, WorkspaceID: "ws-1", UserID: "u-1", SessionID: "s-RECONNECTED",
	}); err != nil {
		t.Fatalf("decline from a reconnected session must still cancel the ring, got: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("HandleInboundInvite returned %v", err)
	}
	if session.HasActiveCall() {
		t.Fatal("LEAK: agent still reserved after declining from a reconnected session")
	}
}

// A decline from a DIFFERENT user must NOT cancel someone else's ring — that stays
// session/user-scoped. The ring is only cleared by its TTL. This guards against
// over-relaxing the match.
func TestInboundDecline_WrongUser_CannotCancel(t *testing.T) {
	session := newInboundTestSession("s-1", "u-1", "ws-1")
	uc := newInboundTestUseCase(t, &inboundTestRegistry{available: []dialer.DialerSession{session}}, &inboundTestAdmission{}, nil)
	done := runInboundInvite(t, uc, &inboundTestDialog{id: "dlg-1", from: "+5511999999999", to: "1000"})

	offer := waitInboundOffer(t, session).Payload.(dialer.InboundCallOffer)

	if err := uc.Decline(context.Background(), dialer.DeclineInboundCallInput{
		OfferID: offer.OfferID, WorkspaceID: "ws-1", UserID: "u-OTHER", SessionID: "s-1",
	}); !errors.Is(err, dialer.ErrInboundOfferNotForUser) {
		t.Fatalf("expected ErrInboundOfferNotForUser for a foreign-user decline, got %v", err)
	}
	if !session.HasActiveCall() {
		t.Fatal("a foreign-user decline must not free the agent")
	}
	<-done // ring clears on TTL
}

// Accept stays session-strict: a call is bridged onto the accepting session, so a
// stale/foreign session must not be able to accept.
func TestInboundAccept_FromWrongSession_Rejected(t *testing.T) {
	session := newInboundTestSession("s-1", "u-1", "ws-1")
	uc := newInboundTestUseCase(t, &inboundTestRegistry{available: []dialer.DialerSession{session}}, &inboundTestAdmission{}, nil)
	done := runInboundInvite(t, uc, &inboundTestDialog{id: "dlg-1", from: "+5511999999999", to: "1000"})

	offer := waitInboundOffer(t, session).Payload.(dialer.InboundCallOffer)

	if err := uc.Accept(context.Background(), dialer.AcceptInboundCallInput{
		OfferID: offer.OfferID, WorkspaceID: "ws-1", UserID: "u-1", SessionID: "s-STALE",
	}); !errors.Is(err, dialer.ErrInboundOfferNotForUser) {
		t.Fatalf("expected accept from a stale session to be rejected, got %v", err)
	}
	<-done
}

// Broker unit: decline matches at user level (delivers the response even when the
// session id differs); accept matches at session level (rejected on mismatch);
// a foreign user is rejected either way; double-resolve is rejected.
func TestInboundOfferBroker_DeclineUserLevel_AcceptSessionLevel(t *testing.T) {
	newState := func() *InboundOfferState {
		return &InboundOfferState{
			Offer:           dialer.InboundCallOffer{OfferID: "off-1", WorkspaceID: "ws-1"},
			TargetUserID:    "u-1",
			TargetSessionID: "s-1",
			Response:        make(chan InboundOfferResponse, 1),
		}
	}

	// Accept with a mismatched session is rejected and delivers nothing.
	b := NewInboundOfferBroker()
	st := newState()
	remove := b.Store(st)
	if err := b.Accept(context.Background(), dialer.AcceptInboundCallInput{OfferID: "off-1", WorkspaceID: "ws-1", UserID: "u-1", SessionID: "s-2"}); !errors.Is(err, dialer.ErrInboundOfferNotForUser) {
		t.Fatalf("accept mismatch: want ErrInboundOfferNotForUser, got %v", err)
	}
	select {
	case <-st.Response:
		t.Fatal("mismatched accept must not deliver a response")
	default:
	}
	remove()

	// Decline with a mismatched session (same user) IS delivered — the fix.
	b = NewInboundOfferBroker()
	st = newState()
	remove = b.Store(st)
	if err := b.Decline(context.Background(), dialer.DeclineInboundCallInput{OfferID: "off-1", WorkspaceID: "ws-1", UserID: "u-1", SessionID: "s-2"}); err != nil {
		t.Fatalf("decline from a different session should still resolve, got %v", err)
	}
	select {
	case resp := <-st.Response:
		if resp.Accepted {
			t.Fatal("decline should deliver Accepted=false")
		}
	case <-time.After(time.Second):
		t.Fatal("user-level decline did not deliver a response")
	}
	// Double-resolve rejected.
	if err := b.Decline(context.Background(), dialer.DeclineInboundCallInput{OfferID: "off-1", WorkspaceID: "ws-1", UserID: "u-1", SessionID: "s-1"}); !errors.Is(err, dialer.ErrInboundOfferAlreadyResolved) {
		t.Fatalf("want ErrInboundOfferAlreadyResolved, got %v", err)
	}
	remove()

	// Decline from a foreign user is rejected and delivers nothing.
	b = NewInboundOfferBroker()
	st = newState()
	remove = b.Store(st)
	if err := b.Decline(context.Background(), dialer.DeclineInboundCallInput{OfferID: "off-1", WorkspaceID: "ws-1", UserID: "u-OTHER", SessionID: "s-1"}); !errors.Is(err, dialer.ErrInboundOfferNotForUser) {
		t.Fatalf("foreign-user decline: want ErrInboundOfferNotForUser, got %v", err)
	}
	select {
	case <-st.Response:
		t.Fatal("foreign-user decline must not deliver a response")
	default:
	}
	remove()
}
