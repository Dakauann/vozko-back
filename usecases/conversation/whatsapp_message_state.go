package conversation_usecase

import (
	"log"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/usecases/messagestate"
)

func quotedProviderMessageID(message *conversation.WhatsAppMessage) string {
	if message == nil || message.Context == nil {
		return ""
	}
	return strings.TrimSpace(message.Context.ID)
}

func (uc *handleWhatsAppMessageUseCase) stateWriter() messagestate.Writer {
	return messagestate.Writer{Messages: uc.messageRepo, Announcer: uc.hub}
}

func (uc *handleWhatsAppMessageUseCase) foldInboundReactions(payload *conversation.WhatsAppWebhookPayload) {
	if payload == nil || uc.messageRepo == nil {
		return
	}
	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			for i := range change.Value.Messages {
				message := &change.Value.Messages[i]
				if message.Reaction == nil || uc.isOutboundMessage(message.From, change.Value.Metadata.PhoneNumberID) {
					continue
				}
				uc.foldReaction(message)
			}
		}
	}
}

func (uc *handleWhatsAppMessageUseCase) foldReaction(message *conversation.WhatsAppMessage) {
	targetID := strings.TrimSpace(message.Reaction.MessageID)
	target, err := uc.messageRepo.GetByWhatsAppMessageID(targetID)
	if err != nil {
		log.Printf("[whatsapp-reaction] reaction %s to %s has no stored target, dropped: %v", message.ID, targetID, err)
		return
	}
	if !isConversationParty(target, message.From) {
		log.Printf("[whatsapp-reaction] reaction %s from %s targets message %s of another conversation, dropped", message.ID, message.From, target.ID)
		return
	}
	reaction := conversation.Reaction{
		Emoji:     message.Reaction.Emoji,
		Direction: conversation.ReactionInbound,
		ActorID:   strings.TrimSpace(message.From),
		At:        parseWhatsAppTimestamp(message.Timestamp),
	}
	if err := target.React(reaction); err != nil {
		log.Printf("[whatsapp-reaction] reaction %s not applied to %s: %v", message.ID, target.ID, err)
		return
	}
	if err := uc.stateWriter().Save(target); err != nil {
		log.Printf("[whatsapp-reaction] reaction %s not stored on %s: %v", message.ID, target.ID, err)
	}
}

func isConversationParty(m *conversation.Message, number string) bool {
	party := lead.NormalizeNumber(number)
	return party != "" && (lead.NormalizeNumber(m.From) == party || lead.NormalizeNumber(m.To) == party)
}

func (uc *handleWhatsAppMessageUseCase) storeTranscript(messageID, transcript string) {
	if uc.messageRepo == nil || messageID == "" {
		return
	}
	audio, err := uc.messageRepo.GetByID(messageID)
	if err != nil {
		log.Printf("[whatsapp-audio] message %s not found for its transcript: %v", messageID, err)
		return
	}
	audio.SetTranscript(transcript)
	if err := uc.stateWriter().Save(audio); err != nil {
		log.Printf("[whatsapp-audio] transcript of %s not stored: %v", messageID, err)
	}
}
