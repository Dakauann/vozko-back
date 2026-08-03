package dialer_usecase

import (
	"context"
	"strings"
	"sync"

	"vozko/domain/dialer"
)

type InboundOfferResponse struct {
	Accepted bool
	Reason   string
}

type InboundOfferState struct {
	Offer           dialer.InboundCallOffer
	TargetUserID    string
	TargetSessionID string
	Response        chan InboundOfferResponse
	resolved        bool
}

type InboundOfferBroker struct {
	mu      sync.Mutex
	pending map[string]*InboundOfferState
}

func NewInboundOfferBroker() *InboundOfferBroker {
	return &InboundOfferBroker{pending: make(map[string]*InboundOfferState)}
}

func (b *InboundOfferBroker) Store(state *InboundOfferState) func() {
	b.mu.Lock()
	b.pending[state.Offer.OfferID] = state
	b.mu.Unlock()
	return func() { b.del(state.Offer.OfferID) }
}

func (b *InboundOfferBroker) del(offerID string) {
	b.mu.Lock()
	delete(b.pending, offerID)
	b.mu.Unlock()
}

func (b *InboundOfferBroker) Accept(_ context.Context, input dialer.AcceptInboundCallInput) error {
	// Accept must come from the exact session the offer was routed to: the call is
	// bridged onto that session, so a stale/foreign session cannot accept it.
	return b.resolve(input.OfferID, input.WorkspaceID, input.UserID, input.SessionID, true,
		InboundOfferResponse{Accepted: true})
}

func (b *InboundOfferBroker) Decline(_ context.Context, input dialer.DeclineInboundCallInput) error {
	// Decline matches at USER level, the session id is NOT required. A dialer that
	// dropped and reconnected gets a fresh session id, but the user rejecting the
	// ring must still cancel it. Requiring an exact session match here left the ring
	// reservation stuck until the offer TTL (~30s), blocking new outbound dials with
	// "you already have an active call".
	return b.resolve(input.OfferID, input.WorkspaceID, input.UserID, input.SessionID, false,
		InboundOfferResponse{Accepted: false, Reason: input.Reason})
}

func (b *InboundOfferBroker) resolve(offerID, workspaceID, userID, sessionID string, requireSession bool, resp InboundOfferResponse) error {
	offerID = strings.TrimSpace(offerID)
	if offerID == "" {
		return dialer.ErrInboundOfferNotFound
	}
	b.mu.Lock()
	state, ok := b.pending[offerID]
	if !ok || state == nil {
		b.mu.Unlock()
		return dialer.ErrInboundOfferNotFound
	}
	if state.Offer.WorkspaceID != workspaceID || state.TargetUserID != userID ||
		(requireSession && state.TargetSessionID != sessionID) {
		b.mu.Unlock()
		return dialer.ErrInboundOfferNotForUser
	}
	if state.resolved {
		b.mu.Unlock()
		return dialer.ErrInboundOfferAlreadyResolved
	}
	state.resolved = true
	b.mu.Unlock()

	select {
	case state.Response <- resp:
		return nil
	default:
		return dialer.ErrInboundOfferAlreadyResolved
	}
}
