package conversation_usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/agent"
	"vozko/domain/ai"
	"vozko/domain/conversation"
	"vozko/domain/redact"
	"vozko/domain/shared"
	toolsdomain "vozko/domain/tools"
)

type whatsAppAgentReply struct {
	agentCtx    *agentContext
	whatsappCtx WhatsAppContext

	entryID        string
	entryType      shared.EntryType
	conversationID string

	recipient       string
	businessNumber  string
	outboundPhoneID string
	receivedPhoneID string

	incomingMessageID string
	query             string
	latest            string
	triggerNote       string
}

func (uc *handleWhatsAppMessageUseCase) turnHistory(r whatsAppAgentReply) ([]ai.Message, error) {
	var history []*conversation.Message
	if uc.messageRepo != nil && r.entryID != "" {
		rows, err := uc.messageRepo.ListByEntry(r.entryID, r.entryType)
		if err != nil {
			return nil, fmt.Errorf("history of %s %s: %w", r.entryType, r.entryID, err)
		}
		history = withoutProviderMessage(rows, r.incomingMessageID)
	}
	messages := uc.composeConversationHistory(history, r.businessNumber, r.recipient)
	if r.latest != "" {
		messages = append(messages, ai.Message{Role: ai.RoleUser, Content: r.latest})
	}
	return messages, nil
}

func withoutProviderMessage(rows []*conversation.Message, providerMessageID string) []*conversation.Message {
	providerMessageID = strings.TrimSpace(providerMessageID)
	if providerMessageID == "" {
		return rows
	}
	out := make([]*conversation.Message, 0, len(rows))
	for _, row := range rows {
		if row != nil && row.ProviderMessageID() == providerMessageID {
			continue
		}
		out = append(out, row)
	}
	return out
}

func (uc *handleWhatsAppMessageUseCase) sendAgentReply(ctx context.Context, r whatsAppAgentReply) error {
	agentCtx := r.agentCtx
	if dec := uc.guardRecordAIResponse(ctx, agentCtx.getWorkspaceID(), r.entryID); dec.Block {
		log.Printf("[whatsapp-usecase] AI reply rate limit hit for entry=%s count=%d, skipping AI response", r.entryID, dec.Count)
		return skippedReply(replySkipRateLimited)
	}

	var messagingModel string
	if agentCtx != nil && agentCtx.agent != nil && agentCtx.agent.MessagingModel != "" {
		messagingModel = agentCtx.agent.MessagingModel
	}

	if !uc.canAffordAIReply(agentCtx.getWorkspaceID(), messagingModel) {
		log.Printf("[whatsapp-usecase] message recorded, but skipping AI response (insufficient balance for workspace %s)", agentCtx.getWorkspaceID())
		return skippedReply(replySkipInsufficientBalance)
	}

	conversationMessages, err := uc.turnHistory(r)
	if err != nil {
		return err
	}

	var campaignPhoneID string
	if agentCtx != nil && agentCtx.wcCampaign != nil {
		campaignPhoneID = agentCtx.wcCampaign.BusinessPhoneID
	}
	outboundClient, err := uc.resolveWhatsAppClient(campaignPhoneID, r.receivedPhoneID)
	if err != nil {
		log.Printf("[whatsapp-usecase] cannot send reply: %v", err)
		return fmt.Errorf("cannot send WhatsApp reply: %w", err)
	}

	initialTypingSentAt := ensureWhatsAppTypingIndicatorFresh(ctx, outboundClient, r.incomingMessageID, "[whatsapp-usecase]", time.Time{})

	turnEntryID, turnEntryType := "", shared.EntryType("")
	if agentCtx != nil {
		turnEntryID, turnEntryType = agentCtx.getEntryInfo()
	}
	generateInput := uc.assembleWhatsAppTurn(ctx, whatsAppTurn{
		agentCtx:        agentCtx,
		whatsappCtx:     r.whatsappCtx,
		RecipientPhone:  r.recipient,
		BusinessPhoneID: r.outboundPhoneID,
		EntryID:         turnEntryID,
		EntryType:       turnEntryType,
		Vars:            agent.MetadataToVars(r.whatsappCtx.Metadata),
		Query:           r.query,
		Messages:        conversationMessages,
		TriggerNote:     r.triggerNote,
		Model:           messagingModel,
		Temperature:     0.2,
		Segmented:       true,
	})
	toolsForAgent := generateInput.Tools

	log.Printf("[whatsapp-usecase] Starting AI response generation (model: %s, tools: %d)", messagingModel, len(toolsForAgent))

	output, err := uc.aiService.Generate(ctx, generateInput)
	if err != nil {
		fmt.Println(err)
		return err
	}

	uc.recordToolCallHistory(ctx, r, output.ToolCalls)

	messages := output.Messages
	if len(messages) == 0 {

		if content := strings.TrimSpace(output.Message.Content); content != "" {
			messages = []string{content}
		}
	}

	for i, m := range messages {
		messages[i] = sanitizeAIOutput(m)
	}
	messages = filterEmptyStrings(messages)
	if len(messages) == 0 {
		log.Printf("[whatsapp-usecase] entry=%s model returned no content, not replying", r.entryID)
		return skippedReply(replySkipEmptyReply)
	}

	for i, msgText := range messages {
		if i == 0 {
			initialTypingSentAt = ensureWhatsAppTypingIndicatorFresh(ctx, outboundClient, r.incomingMessageID, "[whatsapp-usecase]", initialTypingSentAt)
			waitForWhatsAppTypingVisibility(initialTypingSentAt, whatsAppTypingMinVisibleTime)
		} else {
			sendWhatsAppTypingIndicatorWithDelay(ctx, outboundClient, r.incomingMessageID, "[whatsapp-usecase]", 1500*time.Millisecond, time.Second)
		}

		sendInput := conversation.SendTextMessageInput{
			To:   r.recipient,
			Body: msgText,
		}
		sendOutput, err := outboundClient.SendTextMessage(ctx, sendInput)
		if err != nil {
			return err
		}

		if uc.historyManager != nil && (r.conversationID != "" || r.entryID != "") {
			record := conversation.MessageHistoryRecord{
				SentBy:         agentCtx.sentBy(),
				EntryID:        r.entryID,
				EntryType:      r.entryType,
				Channel:        conversation.MessageChannelWhatsApp,
				MessageType:    conversation.MessageTypeAIResponse,
				ConversationID: r.conversationID,
				MessageID:      "",
				From:           r.businessNumber,
				To:             sendInput.To,
				Text:           sendInput.Body,
				Timestamp:      time.Now().UTC(),
			}
			if sendOutput != nil {
				record.MessageID = strings.TrimSpace(sendOutput.MessageID)
				if strings.TrimSpace(record.To) == "" {
					record.To = strings.TrimSpace(sendOutput.ContactWaID)
				}
			}
			if err := uc.historyManager.Record(ctx, record); err != nil {
				return err
			}
			uc.recordAIAttendance(agentCtx, r.entryID, r.entryType, record.MessageID)
		}

		log.Printf("[whatsapp-usecase] ai response:\n%s", msgText)
	}
	return nil
}

func (uc *handleWhatsAppMessageUseCase) recordToolCallHistory(ctx context.Context, r whatsAppAgentReply, toolCalls []ai.ToolCall) {
	if uc.historyManager == nil || (r.conversationID == "" && r.entryID == "") || len(toolCalls) == 0 {
		return
	}
	for _, tc := range toolCalls {
		toolCallText := toolCallHistoryText(tc)
		toolCallRecord := conversation.MessageHistoryRecord{
			SentBy:         r.agentCtx.sentBy(),
			EntryID:        r.entryID,
			EntryType:      r.entryType,
			Channel:        conversation.MessageChannelWhatsApp,
			MessageType:    conversation.MessageTypeToolCall,
			ConversationID: r.conversationID,
			MessageID:      uuid.NewString(),
			From:           "system",
			To:             "system",
			Text:           toolCallText,
			Metadata:       conversation.ToolCallMetadata(tc.ID),
			Timestamp:      time.Now().UTC(),
		}
		if err := uc.historyManager.Record(ctx, toolCallRecord); err != nil {
			log.Printf("[whatsapp-usecase] failed to record tool call: %v", err)
		}

		if tc.Result == nil {
			continue
		}
		toolResultText := toolResultHistoryText(tc.Result)
		if toolResultText == "" {
			continue
		}
		toolResultRecord := conversation.MessageHistoryRecord{
			SentBy:         r.agentCtx.sentBy(),
			EntryID:        r.entryID,
			EntryType:      r.entryType,
			Channel:        conversation.MessageChannelWhatsApp,
			MessageType:    conversation.MessageTypeToolResult,
			ConversationID: r.conversationID,
			MessageID:      uuid.NewString(),
			From:           "system",
			To:             "system",
			Text:           toolResultText,
			Metadata:       conversation.ToolCallMetadata(tc.ID),
			Timestamp:      time.Now().UTC(),
		}
		if err := uc.historyManager.Record(ctx, toolResultRecord); err != nil {
			log.Printf("[whatsapp-usecase] failed to record tool result: %v", err)
		}
	}
}

var toolHistoryRedactor = redact.New()

func toolCallHistoryText(tc ai.ToolCall) string {
	return fmt.Sprintf("[Tool Call] %s: %v", tc.Name, toolHistoryRedactor.Map(tc.Arguments))
}

func toolResultHistoryText(result *toolsdomain.ExecutionResult) string {
	return toolHistoryRedactor.String(toolResultText(result.IsError, result.ContextUpdateText, toolHistoryRedactor.Value(result.Result)))
}

func toolResultText(isError bool, contextText string, payload interface{}) string {
	switch {
	case isError:
		if txt := strings.TrimSpace(contextText); txt != "" {
			return fmt.Sprintf("[Tool Error] %s", txt)
		}
		if payload != nil {
			if resultJSON, err := json.Marshal(payload); err == nil {
				return fmt.Sprintf("[Tool Error] %s", string(resultJSON))
			}
			return fmt.Sprintf("[Tool Error] %v", payload)
		}
		return "[Tool Error] execution failed"
	case contextText != "":
		return fmt.Sprintf("[Tool Result] %s", contextText)
	case payload != nil:
		if resultJSON, err := json.Marshal(payload); err == nil {
			return fmt.Sprintf("[Tool Result] %s", string(resultJSON))
		}
		return fmt.Sprintf("[Tool Result] %v", payload)
	}
	return ""
}
