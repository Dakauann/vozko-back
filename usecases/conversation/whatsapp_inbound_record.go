package conversation_usecase

import (
	"encoding/json"
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
		SentBy:             conversation.SentByContact(message.From),
		EntryID:            target.entryID,
		EntryType:          target.entryType,
		Channel:            conversation.MessageChannelWhatsApp,
		MessageType:        conversation.MessageTypeUserMessage,
		ConversationID:     target.conversationID,
		MessageID:          strings.TrimSpace(message.ID),
		From:               strings.TrimSpace(message.From),
		To:                 target.businessNumber,
		Text:               text,
		Timestamp:          parseWhatsAppTimestamp(message.Timestamp),
		Metadata:           inboundMetadata(message),
		AdReferral:         message.Referral.AdReferral(),
		ReplyToWAMessageID: quotedProviderMessageID(message),
		InteractiveReply:   interactiveChoice(message.Interactive),
	}
	if leadRecord != nil {
		record.SenderName = leadRecord.DisplayName()
		record.SenderAvatar = leadRecord.ProfilePictureURL
	}
	return record
}

func inboundMetadata(message *conversation.WhatsAppMessage) json.RawMessage {
	facts := map[string]any{}
	if located := message.Location.Metadata(); len(located) > 0 {
		if err := json.Unmarshal(located, &facts); err != nil {
			return located
		}
	}
	if b := message.Button; b != nil {
		facts["button"] = map[string]string{"payload": b.Payload, "text": b.Text}
	}
	if c := message.Context; c != nil && strings.TrimSpace(c.ID) != "" {
		facts["context"] = map[string]string{"id": strings.TrimSpace(c.ID)}
	}
	if len(facts) == 0 {
		return nil
	}
	raw, err := json.Marshal(facts)
	if err != nil {
		return message.Location.Metadata()
	}
	return raw
}

func interactiveChoice(i *conversation.WhatsAppInteractive) *conversation.InteractiveReply {
	switch {
	case i == nil:
		return nil
	case i.ButtonReply != nil:
		return &conversation.InteractiveReply{Kind: conversation.InteractiveReplyButton, OptionID: i.ButtonReply.ID, Title: i.ButtonReply.Title}
	case i.ListReply != nil:
		return &conversation.InteractiveReply{Kind: conversation.InteractiveReplyList, OptionID: i.ListReply.ID, Title: i.ListReply.Title}
	}
	return nil
}
