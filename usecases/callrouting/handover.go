package callrouting_usecase

import (
	"context"

	"vozko/domain/callrouting"
	"vozko/domain/callsession"
)

func (d *Dispatcher) SetConversationHandoff(handoff callrouting.ConversationHandoff) {
	d.handoff = handoff
}

func (d *Dispatcher) mayHandOver(ctx context.Context, workspaceID string, call callrouting.RoutedCall, fromUserID, toUserID string) bool {
	if call.Channel() != callsession.OfferChannelWhatsApp {
		return true
	}
	handover, ok := d.handoverOf(workspaceID, call, fromUserID, toUserID)
	return ok && d.handoff.MayHandOver(ctx, handover) == nil
}

func (d *Dispatcher) handoverGate(ctx context.Context, workspaceID string, call callrouting.RoutedCall, fromUserID string) func(callsession.CallSession) bool {
	allowed := map[string]bool{}
	return func(session callsession.CallSession) bool {
		userID := session.UserID()
		if verdict, known := allowed[userID]; known {
			return verdict
		}
		allowed[userID] = d.mayHandOver(ctx, workspaceID, call, fromUserID, userID)
		return allowed[userID]
	}
}

func (d *Dispatcher) connect(ctx context.Context, workspaceID string, call callrouting.RoutedCall, session callsession.CallSession, fromUserID string) error {
	if err := call.Connect(session); err != nil {
		return err
	}
	if call.Channel() != callsession.OfferChannelWhatsApp {
		return nil
	}
	handover, ok := d.handoverOf(workspaceID, call, fromUserID, session.UserID())
	if !ok {
		d.deps.Logger.Printf("[CallRouting] call %s reached %s but its conversation cannot follow", call.ID(), session.UserID())
		return nil
	}
	if err := d.handoff.HandOver(ctx, handover); err != nil {
		d.deps.Logger.Printf("[CallRouting] call %s reached %s but its conversation stayed with %s: %v", call.ID(), session.UserID(), fromUserID, err)
	}
	return nil
}

func (d *Dispatcher) handoverOf(workspaceID string, call callrouting.RoutedCall, fromUserID, toUserID string) (callrouting.Handover, bool) {
	contact, known := call.Contact()
	if d.handoff == nil || !known || fromUserID == "" {
		return callrouting.Handover{}, false
	}
	return callrouting.Handover{WorkspaceID: workspaceID, Contact: contact, FromUserID: fromUserID, ToUserID: toUserID}, true
}
