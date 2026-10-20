package conversation_usecase

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/whatsappform"
)

var errFormRepliesUnwired = errors.New("conversation: a form answer arrived but no form receiver is wired")

type InboundFormReply struct {
	WorkspaceID      string
	BusinessPhoneID  string
	ContactNumber    string
	EntryID          string
	EntryType        string
	ConversationID   string
	LeadID           string
	InboundMessageID string
	ContextMessageID string
	ResponseJSON     string
	SubmittedAt      time.Time
}

type ReceivedFormReply struct {
	Text         string
	Metadata     map[string]any
	ResumedRunID string
}

type FormReplyReceiver interface {
	ReceiveFormReply(ctx context.Context, in InboundFormReply) (ReceivedFormReply, error)
}

func (uc *handleWhatsAppMessageUseCase) SetFormReplies(r FormReplyReceiver) {
	uc.formReplies = r
}

type formReplyTarget struct {
	workspaceID     string
	entryID         string
	entryType       shared.EntryType
	conversationID  string
	leadID          string
	businessPhoneID string
}

func contactNumberOf(from string) string {
	return shared.EnsureDialablePhoneNumber(strings.TrimSpace(from))
}

func (uc *handleWhatsAppMessageUseCase) receiveFormReply(ctx context.Context, message *conversation.WhatsAppMessage, target formReplyTarget) (*ReceivedFormReply, error) {
	reply, ok := message.FormReply()
	if !ok {
		return nil, nil
	}
	if target.entryID == "" || target.workspaceID == "" {
		log.Printf("[whatsapp-forms] answer %s has no conversation to land in", message.ID)
		return nil, nil
	}
	if uc.formReplies == nil {
		return nil, errFormRepliesUnwired
	}
	in := InboundFormReply{
		WorkspaceID: target.workspaceID, BusinessPhoneID: target.businessPhoneID, ContactNumber: contactNumberOf(message.From),
		EntryID: target.entryID, EntryType: string(target.entryType), ConversationID: target.conversationID, LeadID: target.leadID,
		InboundMessageID: strings.TrimSpace(message.ID), ResponseJSON: reply.ResponseJSON, SubmittedAt: parseWhatsAppTimestamp(message.Timestamp),
	}
	if message.Context != nil {
		in.ContextMessageID = strings.TrimSpace(message.Context.ID)
	}
	received, err := uc.formReplies.ReceiveFormReply(ctx, in)
	if errors.Is(err, whatsappform.ErrResponseInvalid) {
		log.Printf("[whatsapp-forms] answer %s could not be read: %v", message.ID, err)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(received.Text) != "" && message.Text != nil {
		message.Text.Body = received.Text
	}
	return &received, nil
}

func withFormCard(record conversation.MessageHistoryRecord, received *ReceivedFormReply) (conversation.MessageHistoryRecord, error) {
	if received == nil {
		return record, nil
	}
	for key, value := range received.Metadata {
		merged, err := conversation.WithMetadataValue(record.Metadata, key, value)
		if err != nil {
			return record, err
		}
		record.Metadata = merged
	}
	return record, nil
}
