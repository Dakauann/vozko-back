package ws

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"vozko/domain/callrouting"
	callrouting_usecase "vozko/usecases/callrouting"
)

type CallTransfers interface {
	Transfer(ctx context.Context, input callrouting_usecase.TransferInput) (string, error)
	Cancel(workspaceID, userID, callID string) error
}

type CallTransferCancelPayload struct {
	CallID string `json:"call_id"`
}

type CallTransferPayload struct {
	TargetKind string `json:"target_kind"`
	QueueID    string `json:"queue_id,omitempty"`
	UserID     string `json:"user_id,omitempty"`
	Notes      string `json:"notes,omitempty"`
}

var transferErrorCodes = []struct {
	err  error
	code string
}{
	{callrouting.ErrCallNotFound, "call_not_found"},
	{callrouting.ErrNotCallOwner, "not_call_owner"},
	{callrouting.ErrTargetUnavailable, "target_unavailable"},
	{callrouting.ErrConversationOutOfReach, "conversation_out_of_reach"},
	{callrouting.ErrTransferToSelf, "transfer_to_self"},
	{callrouting.ErrTransferInProgress, "transfer_in_progress"},
	{callrouting.ErrNoTransferToCancel, "no_transfer"},
	{callrouting.ErrQueueNotFound, "queue_not_found"},
	{callrouting.ErrTransferNotesTooLong, "notes_too_long"},
	{callrouting.ErrInvalidTransferTarget, "invalid_target"},
	{callrouting.ErrTransferNotAllowed, "unauthorized"},
}

func (h *CallSessionWSHandler) WithTransfers(transfers CallTransfers) *CallSessionWSHandler {
	h.transfers = transfers
	return h
}

func (h *CallSessionWSHandler) WithReconnects(reconnects CallReconnects) *CallSessionWSHandler {
	h.reconnects = reconnects
	return h
}

func (h *CallSessionWSHandler) handleTransfer(session *callSession, raw json.RawMessage) {
	callID, ok := h.transferableCall(session)
	if !ok {
		return
	}
	var payload CallTransferPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		session.send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "invalid_payload", Message: "Invalid transfer payload"}})
		return
	}
	_, err := h.transfers.Transfer(context.Background(), callrouting_usecase.TransferInput{
		WorkspaceID: session.workspaceID,
		UserID:      session.userID,
		CallID:      callID,
		Target: callrouting.TransferTarget{
			Kind:    callrouting.TargetKind(payload.TargetKind),
			QueueID: payload.QueueID,
			UserID:  payload.UserID,
		},
		Notes: payload.Notes,
	})
	h.sendTransferError(session, callID, err)
}

func (h *CallSessionWSHandler) handleTransferCancel(session *callSession, raw json.RawMessage) {
	if !h.transfersEnabled(session) {
		return
	}
	var payload CallTransferCancelPayload
	if err := json.Unmarshal(raw, &payload); err != nil || strings.TrimSpace(payload.CallID) == "" {
		session.send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "missing_fields", Message: "call_id is required"}})
		return
	}
	callID := strings.TrimSpace(payload.CallID)
	h.sendTransferError(session, callID, h.transfers.Cancel(session.workspaceID, session.userID, callID))
}

func (h *CallSessionWSHandler) transfersEnabled(session *callSession) bool {
	if h.transfers == nil {
		session.send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "transfer_unavailable", Message: "Call transfers are not enabled on this server"}})
		return false
	}
	return true
}

func (h *CallSessionWSHandler) transferableCall(session *callSession) (string, bool) {
	if !h.transfersEnabled(session) {
		return "", false
	}
	lc := session.Current()
	if lc == nil || lc.call == nil {
		session.send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "no_call_to_transfer", Message: "No active call to transfer"}})
		return "", false
	}
	return lc.call.ID(), true
}

func (h *CallSessionWSHandler) sendTransferError(session *callSession, callID string, err error) {
	if err == nil {
		return
	}
	code := "transfer_failed"
	for _, known := range transferErrorCodes {
		if errors.Is(err, known.err) {
			code = known.code
			break
		}
	}
	message := err.Error()
	if code == "transfer_failed" {
		h.logger.Printf("[CallSessionWS] transfer of call %s failed: %v", callID, err)
		message = "The transfer could not be started"
	}
	session.send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: code, Message: message, EntryID: callID}})
}
