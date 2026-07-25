package ws

import (
	dialer_domain "vozko/domain/dialer"
)

type callTrunkBusyContext struct {
	EntryID     string
	EntryType   string
	PhoneNumber string
	RequestID   string
}

func buildCallTrunkBusyMessage(busy *dialer_domain.ErrTrunkBusy, ctx callTrunkBusyContext) *WSOutgoingMessage {
	if busy == nil {
		return nil
	}
	retryMs := int(busy.RetryAfter.Milliseconds())
	if retryMs <= 0 {
		retryMs = 3000
	}
	reason := busy.Reason
	if reason == "" {
		reason = dialer_domain.TrunkBusyReasonNotOwned
	}
	return &WSOutgoingMessage{
		Type: WSEventCallTrunkBusy,
		Payload: CallTrunkBusyPayload{
			TrunkID:      busy.TrunkID,
			RetryAfterMs: retryMs,
			Reason:       reason,
			EntryID:      ctx.EntryID,
			EntryType:    ctx.EntryType,
			Phone:        ctx.PhoneNumber,
			RequestID:    ctx.RequestID,
		},
	}
}
