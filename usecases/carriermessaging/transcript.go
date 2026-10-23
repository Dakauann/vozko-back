package carriermessaging_usecase

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"vozko/domain/carriermessaging"
	"vozko/domain/conversation"
	"vozko/domain/rcs"
)

func (s *Service) recordOutbound(ctx context.Context, m carriermessaging.Message, from string, sentBy conversation.SentBy, at time.Time) {
	payload := map[string]any{"reference": m.Reference, "step": m.Step}
	text := m.Text
	if m.Channel == carriermessaging.ChannelRCS {
		payload["class"], payload["content"] = m.Class, m.Content
		text = m.Content.PreviewText()
	} else {
		payload["segments"] = m.Segments
	}
	metadata, err := json.Marshal(map[string]any{string(m.Channel): payload})
	if err != nil {
		log.Printf("[carrier] transcript metadata %s: %v", m.Reference, err)
		return
	}
	record := conversation.MessageHistoryRecord{
		SentBy: sentBy, EntryID: m.ConversationID, EntryType: m.Channel.EntryType(),
		Channel: conversation.MessageChannel(m.Channel), MessageType: messageTypeOf(m.RunKind),
		From: from, To: m.Phone, Text: text, Timestamp: at,
		ProviderMessageID: m.Reference, Metadata: metadata, Read: true,
	}
	if media := m.Content.Media; media != nil {
		record.MediaURL, record.MediaType = media.URL, transcriptMediaType(media.Kind)
	}
	if err := s.Transcript.Record(ctx, record); err != nil {
		log.Printf("[carrier] transcript record %s: %v", m.Reference, err)
	}
}

func (s *Service) recordInbound(ctx context.Context, channel carriermessaging.Channel, conversationID, phone, name, providerMessageID, text string, media *carriermessaging.InboundMedia, at time.Time) {
	record := conversation.MessageHistoryRecord{
		SentBy: conversation.SentByContact(phone), EntryID: conversationID, EntryType: channel.EntryType(),
		Channel: conversation.MessageChannel(channel), MessageType: conversation.MessageTypeUserMessage,
		From: phone, Text: text, Timestamp: at, ProviderMessageID: providerMessageID, SenderName: name,
	}
	if media != nil {
		record.MessageType = conversation.MessageTypeMedia
		record.MediaURL = media.URL
		record.MediaType = conversation.MediaTypeDocument
		if kind, ok := rcs.MediaKindFor(media.MIMEType); ok {
			record.MediaType = transcriptMediaType(kind)
		}
	}
	if err := s.Transcript.Record(ctx, record); err != nil {
		log.Printf("[carrier] transcript inbound %s: %v", providerMessageID, err)
	}
}

func (s *Service) publishStatus(m carriermessaging.Message) {
	if m.Step == carriermessaging.StepWhatsApp || m.ConversationID == "" {
		return
	}
	status := transcriptStatus(m.Status)
	if status == conversation.DeliveryStatusNone {
		return
	}
	if err := s.TranscriptStatus.UpdateDeliveryStatusWithReason(m.Reference, status, 0, m.ErrorCode); err != nil {
		log.Printf("[carrier] transcript status %s: %v", m.Reference, err)
		return
	}
	s.Announcer.BroadcastMessageStatus(m.ConversationID, string(m.Channel.EntryType()), m.Reference, status)
}

func transcriptStatus(status carriermessaging.Status) conversation.DeliveryStatus {
	switch {
	case status == carriermessaging.StatusAccepted:
		return conversation.DeliveryStatusSent
	case status == carriermessaging.StatusDelivered:
		return conversation.DeliveryStatusDelivered
	case status == carriermessaging.StatusRead:
		return conversation.DeliveryStatusRead
	case status.Terminal():
		return conversation.DeliveryStatusFailed
	}
	return conversation.DeliveryStatusNone
}

func messageTypeOf(kind carriermessaging.RunKind) conversation.MessageType {
	if kind == carriermessaging.RunOperator || kind == carriermessaging.RunTest {
		return conversation.MessageTypeOperator
	}
	return conversation.MessageTypeTemplate
}

func transcriptMediaType(kind rcs.MediaKind) conversation.MediaType {
	switch kind {
	case rcs.MediaImage:
		return conversation.MediaTypeImage
	case rcs.MediaVideo:
		return conversation.MediaTypeVideo
	case rcs.MediaAudio:
		return conversation.MediaTypeAudio
	}
	return conversation.MediaTypeDocument
}
