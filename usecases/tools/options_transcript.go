package tools_usecase

import (
	"context"
	"log"
	"strings"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

func (uc *sendWhatsappButtonMessageTool) SetHistory(h conversation.MessageHistoryManager) {
	uc.history = h
}

func (uc *sendWhatsappButtonMessageTool) recordOptions(ctx context.Context, config map[string]interface{}, record conversation.MessageHistoryRecord) {
	if uc.history == nil || strings.TrimSpace(record.EntryID) == "" {
		return
	}
	agentID, _ := config["__agent_id"].(string)
	record.SentBy = conversation.SentByAI(strings.TrimSpace(agentID))
	record.Channel = conversation.MessageChannel(record.EntryType)
	record.MessageType = conversation.MessageTypeAIResponse
	record.Timestamp = time.Now().UTC()
	if err := uc.history.Record(ctx, record); err != nil {
		log.Printf("[Options Tool] the sent options were not recorded on %s entry %s: %v", record.EntryType, record.EntryID, err)
	}
}

func officialOptionsRecord(config map[string]interface{}, input conversation.SendButtonMessageInput, messageID string) conversation.MessageHistoryRecord {
	entryID, _ := config["__entry_id"].(string)
	if contact, _ := config["__recipient_phone"].(string); contact == "" || contact != input.To {
		entryID = ""
	}
	from, _ := config["__business_phone_id"].(string)
	content := conversation.ButtonMessageContent(input)
	return conversation.MessageHistoryRecord{
		EntryID:     strings.TrimSpace(entryID),
		EntryType:   shared.EntryTypeWhatsApp,
		MessageID:   strings.TrimSpace(messageID),
		From:        strings.TrimSpace(from),
		To:          input.To,
		Text:        input.BodyText,
		Interactive: &content,
	}
}

func adapterOptionsRecord(ec *conversation.EntryContext, req conversation.SendInteractiveRequest, outcome *conversation.SendOutcome) conversation.MessageHistoryRecord {
	record := conversation.MessageHistoryRecord{
		EntryID:   ec.EntryID,
		EntryType: ec.EntryType,
		From:      ec.AccountID,
		To:        ec.ContactRef,
		Text:      req.Body,
	}
	if outcome != nil {
		record.ProviderMessageID = outcome.ProviderMessageID
		record.Interactive = outcome.Interactive
	}
	return record
}
