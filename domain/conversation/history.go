package conversation

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"vozko/domain/shared"
)

type MessageHistoryDirection string

const (
	MessageDirectionInbound  MessageHistoryDirection = "INBOUND"
	MessageDirectionOutbound MessageHistoryDirection = "OUTBOUND"
	MessageDirectionUnknown  MessageHistoryDirection = ""
)

func (d MessageHistoryDirection) Valid() bool {
	return d == MessageDirectionInbound || d == MessageDirectionOutbound
}

func (d MessageHistoryDirection) IsOutbound() bool {
	return d == MessageDirectionOutbound
}

type MessageHistoryRecord struct {
	SentBy      SentBy
	EntryID     string
	EntryType   shared.EntryType
	Channel     MessageChannel
	MessageType MessageType

	ConversationID string

	MessageID          string
	ReplyToWAMessageID string
	From               string
	To                 string
	Text               string
	Timestamp          time.Time

	ProviderMessageID string

	MediaID   string
	MediaType MediaType
	MediaURL  string

	Metadata json.RawMessage

	SenderName   string
	SenderAvatar string

	Read   bool
	Silent bool

	AdReferral *AdReferral
}

func (r MessageHistoryRecord) GetEntryID() string {
	if r.EntryID != "" {
		return r.EntryID
	}
	return r.ConversationID
}

func (r MessageHistoryRecord) Message() *Message {
	timestamp := r.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}
	entryType := r.EntryType
	if !entryType.Valid() {
		entryType = shared.EntryTypeWhatsApp
	}
	channel := r.Channel
	if !channel.Valid() {
		channel = MessageChannelWhatsApp
	}
	msgType := r.MessageType
	if !msgType.Valid() {
		msgType = MessageTypeAIResponse
		if r.SentBy.IsContact() {
			msgType = MessageTypeUserMessage
		}
	}
	message := &Message{
		EntryID:      strings.TrimSpace(r.GetEntryID()),
		EntryType:    entryType,
		Channel:      channel,
		MessageType:  msgType,
		Direction:    r.SentBy.Direction(),
		SentBy:       r.SentBy,
		From:         strings.TrimSpace(r.From),
		To:           strings.TrimSpace(r.To),
		Text:         strings.TrimSpace(r.Text),
		Metadata:     r.Metadata,
		SenderName:   strings.TrimSpace(r.SenderName),
		SenderAvatar: strings.TrimSpace(r.SenderAvatar),
		CreatedAt:    timestamp,
		UpdatedAt:    timestamp,
	}
	if r.MediaID != "" {
		mediaID := r.MediaID
		message.MediaID = &mediaID
		message.MediaType = r.MediaType
	}
	if r.Read {
		readAt := time.Now().UTC()
		message.Read = true
		message.ReadAt = &readAt
	}
	if providerID := strings.TrimSpace(r.ProviderMessageID); providerID != "" {
		message.ExternalMessageID = &providerID
	} else if wamid := strings.TrimSpace(r.MessageID); wamid != "" {
		message.WhatsAppMessageID = &wamid
	}
	return message
}

type MessageHistoryManager interface {
	Record(ctx context.Context, record MessageHistoryRecord) error
}
