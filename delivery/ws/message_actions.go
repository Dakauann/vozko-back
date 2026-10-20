package ws

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"runtime/debug"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

const (
	messageActionNotAllowed    = "not_allowed"
	messageActionNotFound      = "not_found"
	messageActionInvalid       = "invalid"
	messageActionUnsupported   = "unsupported"
	messageActionWindowExpired = "window_expired"
	messageActionFailed        = "failed"
)

var errMessageActionPanicked = errors.New("internal error running the message action")

func (h *ConversationHub) SetMessageActions(actions conversation.MessageActionUseCases) {
	h.messageActions = actions
}

func (h *ConversationHub) messageCapabilities(entryType string) conversation.MessageCapabilities {
	if h.messageActions.Capabilities == nil {
		return conversation.MessageCapabilities{}
	}
	return h.messageActions.Capabilities.MessageCapabilities(shared.EntryType(entryType))
}

func (p MessageActionPayload) target(conn *WSConnection) conversation.MessageTarget {
	return conversation.MessageTarget{
		WorkspaceID: conn.WorkspaceID,
		EntryID:     strings.TrimSpace(p.EntryID),
		EntryType:   shared.EntryType(strings.ToLower(strings.TrimSpace(p.EntryType))),
		MessageID:   strings.TrimSpace(p.MessageID),
	}
}

func (p MessageActionPayload) complete() bool {
	return strings.TrimSpace(p.EntryID) != "" && strings.TrimSpace(p.EntryType) != "" && strings.TrimSpace(p.MessageID) != ""
}

func connPerson(conn *WSConnection) shared.Person {
	return shared.Person{UserID: conn.UserID, SystemAdmin: conn.IsAdmin}
}

func (h *ConversationHub) handleReactMessage(conn *WSConnection, payload json.RawMessage) {
	var p ReactMessagePayload
	if !h.decodeMessageAction(conn, payload, &p, &p.MessageActionPayload) {
		return
	}
	if h.refuseUnconfigured(conn, p.MessageActionPayload, h.messageActions.React == nil) {
		return
	}
	h.runMessageAction(conn, p.MessageActionPayload, func(ctx context.Context, by shared.Person, target conversation.MessageTarget) error {
		_, err := h.messageActions.React.Execute(ctx, by, target, p.Emoji)
		return err
	})
}

func (h *ConversationHub) handleEditMessage(conn *WSConnection, payload json.RawMessage) {
	var p EditMessagePayload
	if !h.decodeMessageAction(conn, payload, &p, &p.MessageActionPayload) {
		return
	}
	if h.refuseUnconfigured(conn, p.MessageActionPayload, h.messageActions.Edit == nil) {
		return
	}
	h.runMessageAction(conn, p.MessageActionPayload, func(ctx context.Context, by shared.Person, target conversation.MessageTarget) error {
		_, err := h.messageActions.Edit.Execute(ctx, by, target, p.Text)
		return err
	})
}

func (h *ConversationHub) handleDeleteMessage(conn *WSConnection, payload json.RawMessage) {
	var p MessageActionPayload
	if !h.decodeMessageAction(conn, payload, &p, &p) {
		return
	}
	if h.refuseUnconfigured(conn, p, h.messageActions.Delete == nil) {
		return
	}
	h.runMessageAction(conn, p, func(ctx context.Context, by shared.Person, target conversation.MessageTarget) error {
		_, err := h.messageActions.Delete.Execute(ctx, by, target)
		return err
	})
}

func (h *ConversationHub) handleResendMessage(conn *WSConnection, payload json.RawMessage) {
	var p MessageActionPayload
	if !h.decodeMessageAction(conn, payload, &p, &p) {
		return
	}
	if h.refuseUnconfigured(conn, p, h.messageActions.Resend == nil) {
		return
	}
	h.runMessageAction(conn, p, func(ctx context.Context, by shared.Person, target conversation.MessageTarget) error {
		sent, err := h.messageActions.Resend.Execute(ctx, by, target, p.RequestID)
		switch {
		case errors.Is(err, conversation.ErrSendRequestInFlight):
			log.Printf("[ConversationHub] resend %s for %s:%s is already in flight", p.RequestID, target.EntryType, target.EntryID)
			return nil
		case errors.Is(err, conversation.ErrResendDeliveryFailed):
			h.BroadcastMessageError(conn.UserID, p.RequestID, target.EntryID, string(target.EntryType), err.Error(), "send_failed")
			return nil
		case err != nil:
			return err
		}
		h.announceOperatorSend(conn, p.RequestID, target.EntryID, string(target.EntryType), sent)
		return nil
	})
}

func (h *ConversationHub) decodeMessageAction(conn *WSConnection, payload json.RawMessage, into any, action *MessageActionPayload) bool {
	if err := json.Unmarshal(payload, into); err != nil {
		h.sendMessageActionError(conn, *action, messageActionInvalid, "Invalid message action payload")
		return false
	}
	if !action.complete() {
		h.sendMessageActionError(conn, *action, messageActionInvalid, "entry_id, entry_type and message_id are required")
		return false
	}
	return true
}

func (h *ConversationHub) refuseUnconfigured(conn *WSConnection, p MessageActionPayload, missing bool) bool {
	if missing {
		h.sendMessageActionError(conn, p, messageActionUnsupported, "Message actions are not available on this server")
	}
	return missing
}

func (h *ConversationHub) runMessageAction(conn *WSConnection, p MessageActionPayload, act func(context.Context, shared.Person, conversation.MessageTarget) error) {
	target, by := p.target(conn), connPerson(conn)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[ConversationHub] PANIC in message action (entry=%s type=%s user=%s): %v\n%s",
					target.EntryID, target.EntryType, conn.UserID, r, debug.Stack())
				h.sendMessageActionError(conn, p, messageActionFailed, errMessageActionPanicked.Error())
			}
		}()
		if err := act(context.Background(), by, target); err != nil {
			h.sendMessageActionError(conn, p, messageActionErrorCode(err), err.Error())
		}
	}()
}

func (h *ConversationHub) sendMessageActionError(conn *WSConnection, p MessageActionPayload, code, message string) {
	h.sendToConnection(conn, &WSOutgoingMessage{
		Type: WSEventError,
		Payload: ErrorPayload{
			Code:      code,
			Message:   message,
			RequestID: p.RequestID,
			EntryID:   p.EntryID,
			EntryType: p.EntryType,
		},
	})
}

func messageActionErrorCode(err error) string {
	switch {
	case errors.Is(err, conversation.ErrUnauthorized),
		errors.Is(err, conversation.ErrMessageActionNotAllowed),
		errors.Is(err, conversation.ErrResendNotAllowed):
		return messageActionNotAllowed
	case errors.Is(err, conversation.ErrMessageNotFound):
		return messageActionNotFound
	case errors.Is(err, conversation.ErrMessageIDRequired),
		errors.Is(err, conversation.ErrEntryIDRequired),
		errors.Is(err, conversation.ErrEntryTypeInvalid),
		errors.Is(err, conversation.ErrMessageEditTextInvalid),
		errors.Is(err, conversation.ErrClientRequestIDRequired),
		errors.Is(err, conversation.ErrClientRequestIDInvalid):
		return messageActionInvalid
	case errors.Is(err, conversation.ErrMessageActionUnsupported),
		errors.Is(err, conversation.ErrCapabilityUnsupported),
		errors.Is(err, conversation.ErrNoAdapterForEntryType):
		return messageActionUnsupported
	case errors.Is(err, conversation.ErrMessageActionWindowExpired),
		errors.Is(err, conversation.ErrOutboundWindowClosed),
		errors.Is(err, conversation.ErrWindowClosed):
		return messageActionWindowExpired
	}
	return messageActionFailed
}
