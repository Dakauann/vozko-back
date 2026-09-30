package callrouting_usecase

import (
	"context"
	"time"

	"vozko/domain/callrouting"
	"vozko/domain/callsession"
	callsession_usecase "vozko/usecases/callsession"
)

const (
	ownerReconnectGrace = 30 * time.Second
	ownerReconnectRing  = 10 * time.Second
)

func (uc *TransferCall) HoldForOwner(ctx context.Context, call callrouting.RoutedCall, userID string) error {
	music, err := uc.holdMusic(ctx, call.WorkspaceID())
	if err != nil {
		return err
	}
	if err := call.Hold(music); err != nil {
		return err
	}
	go uc.awaitOwner(call, userID)
	return nil
}

func (uc *TransferCall) awaitOwner(call callrouting.RoutedCall, userID string) {
	ctx := context.Background()
	deadline := uc.now().Add(uc.reconnectGrace)
	for uc.now().Before(deadline) {
		if callerLeft(call) {
			return
		}
		session := uc.deps.Dispatcher.answerers(call.WorkspaceID(), call.Channel())[userID]
		if session == nil {
			uc.deps.Dispatcher.pause(ctx, call, deadline)
			continue
		}
		outcome := uc.deps.Ringer.Ring(ctx, callsession_usecase.RingRequest{
			Offer: callsession.InboundCallOffer{
				CallID:      call.ID(),
				WorkspaceID: call.WorkspaceID(),
				FromNumber:  call.RemoteNumber(),
				Channel:     call.Channel(),
				Resume:      true,
			},
			Candidates:   []callsession.CallSession{session},
			PerCandidate: min(ownerReconnectRing, time.Until(deadline)),
			Deadline:     deadline,
			CallerGone:   call.Done(),
		})
		if outcome.CallerGone {
			return
		}
		if outcome.Session != nil {
			err := call.Connect(outcome.Session)
			outcome.Session.Release(outcome.OfferID)
			if err == nil {
				return
			}
			uc.deps.Logger.Printf("[CallReconnect] could not give call %s back to %s: %v", call.ID(), userID, err)
		}
		if outcome.DeclinedByUserID != "" {
			break
		}
	}
	_ = call.Hangup()
}
