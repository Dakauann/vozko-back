package callsession_usecase

import (
	"context"
	"time"

	"github.com/google/uuid"

	"vozko/domain/callsession"
)

type RingRequest struct {
	Offer        callsession.InboundCallOffer
	Candidates   []callsession.CallSession
	PerCandidate time.Duration
	Deadline     time.Time
	CallerGone   <-chan struct{}
	OnReserved   func(callsession.CallSession)
}

type RingOutcome struct {
	Session          callsession.CallSession
	OfferID          string
	CallerGone       bool
	DeclinedByUserID string
}

type InboundRinger struct {
	broker *InboundOfferBroker
}

func NewInboundRinger(broker *InboundOfferBroker) *InboundRinger {
	return &InboundRinger{broker: broker}
}

func (r *InboundRinger) Ring(ctx context.Context, req RingRequest) RingOutcome {
	var lastDeclinedBy string
	for _, cand := range req.Candidates {
		if cand == nil || cand.HasActiveCall() {
			continue
		}
		ring := min(req.PerCandidate, time.Until(req.Deadline))
		if ring <= 0 {
			break
		}
		offer := req.Offer
		offer.OfferID = uuid.NewString()
		offer.ExpiresAt = time.Now().Add(ring)

		var onReserved func()
		if req.OnReserved != nil {
			onReserved = func() { req.OnReserved(cand) }
		}
		switch r.ringCandidate(ctx, cand, offer, req.CallerGone, ring, onReserved) {
		case candAccepted:
			return RingOutcome{Session: cand, OfferID: offer.OfferID}
		case candTerminated:
			return RingOutcome{CallerGone: true}
		case candDeclined:
			lastDeclinedBy = cand.UserID()
		}
	}
	return RingOutcome{DeclinedByUserID: lastDeclinedBy}
}

type candidateOutcome int

const (
	candUnavailable candidateOutcome = iota
	candAccepted
	candDeclined
	candTimedOut
	candTerminated
)

func (r *InboundRinger) ringCandidate(
	ctx context.Context,
	cand callsession.CallSession,
	offer callsession.InboundCallOffer,
	callerGone <-chan struct{},
	ring time.Duration,
	onReserved func(),
) candidateOutcome {
	if !cand.Reserve(offer.OfferID) {
		return candUnavailable
	}
	accepted := false
	defer func() {
		if !accepted {
			cand.Release(offer.OfferID)
		}
	}()

	state := &InboundOfferState{
		Offer:           offer,
		TargetUserID:    cand.UserID(),
		TargetSessionID: cand.ID(),
		Response:        make(chan InboundOfferResponse, 1),
	}
	remove := r.broker.Store(state)
	defer remove()

	if onReserved != nil {
		onReserved()
	}
	if err := cand.Notify(callsession.CallSessionControlMessage{Type: callsession.CallSessionInboundCall, Payload: offer}); err != nil {
		return candUnavailable
	}

	timer := time.NewTimer(ring)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		withdraw(cand, offer, withdrawnCallerHungUp)
		return candTerminated
	case <-callerGone:
		withdraw(cand, offer, withdrawnCallerHungUp)
		return candTerminated
	case resp := <-state.Response:
		if resp.Accepted {
			accepted = true
			return candAccepted
		}
		return candDeclined
	case <-timer.C:
		withdraw(cand, offer, withdrawnNoAnswer)
		return candTimedOut
	}
}

const (
	withdrawnNoAnswer     = "no_answer"
	withdrawnCallerHungUp = "caller_hung_up"
)

func withdraw(cand callsession.CallSession, offer callsession.InboundCallOffer, reason string) {
	_ = cand.Notify(callsession.CallSessionControlMessage{
		Type:    callsession.CallSessionInboundCallWithdrawn,
		Payload: callsession.InboundOfferWithdrawn{OfferID: offer.OfferID, Reason: reason},
	})
}
