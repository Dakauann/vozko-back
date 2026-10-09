package conversation_usecase

import (
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/shared"
)

type inboundTarget struct {
	entryID        string
	entryType      shared.EntryType
	conversationID string
	businessNumber string
}

func (uc *handleWhatsAppMessageUseCase) inboundTextRecord(message *conversation.WhatsAppMessage, target inboundTarget, leadRecord *lead.Lead) conversation.MessageHistoryRecord {
	var text string
	if message.Text != nil {
		text = strings.TrimSpace(message.Text.Body)
	}
	record := conversation.MessageHistoryRecord{
		SentBy:         conversation.SentByContact(message.From),
		EntryID:        target.entryID,
		EntryType:      target.entryType,
		Channel:        conversation.MessageChannelWhatsApp,
		MessageType:    conversation.MessageTypeUserMessage,
		ConversationID: target.conversationID,
		MessageID:      strings.TrimSpace(message.ID),
		From:           strings.TrimSpace(message.From),
		To:             target.businessNumber,
		Text:           text,
		Timestamp:      parseWhatsAppTimestamp(message.Timestamp),
		Metadata:       message.Location.Metadata(),
		AdReferral:     message.Referral.AdReferral(),
	}
	if leadRecord != nil {
		record.SenderName = leadRecord.DisplayName()
		record.SenderAvatar = leadRecord.ProfilePictureURL
	}
	return record
}
