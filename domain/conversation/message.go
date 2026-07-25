package conversation

import (
	"encoding/json"
	"strings"
	"time"

	"vozko/domain/shared"
)

type MessageChannel string

const (
	MessageChannelVoice    MessageChannel = "voice"
	MessageChannelWhatsApp MessageChannel = "whatsapp"
	MessageChannelSupport  MessageChannel = "support"
)

func (c MessageChannel) Valid() bool {
	switch c {
	case MessageChannelVoice, MessageChannelWhatsApp, MessageChannelSupport:
		return true
	}
	return false
}

type MessageType string

const (
	MessageTypeUserMessage MessageType = "user_message"
	MessageTypeAIResponse  MessageType = "ai_response"
	MessageTypeToolCall    MessageType = "tool_call"
	MessageTypeToolResult  MessageType = "tool_result"
	MessageTypeAudio       MessageType = "audio"
	MessageTypeSystem      MessageType = "system"
	MessageTypeMedia       MessageType = "media"
	MessageTypeOperator    MessageType = "operator"
	MessageTypeTemplate    MessageType = "template"

	MessageTypeCallPermissionRequest  MessageType = "call_permission_request"
	MessageTypeCallPermissionGranted  MessageType = "call_permission_granted"
	MessageTypeCallPermissionRejected MessageType = "call_permission_rejected"

	// Inbound WhatsApp call lifecycle, recorded into the conversation like a
	// phone call log. These are event/system markers — never conversational turns
	// (see IsCallEvent / AI history exclusion) so they don't pollute AI context.
	MessageTypeCallReceived MessageType = "call_received"
	MessageTypeCallAnswered MessageType = "call_answered"
	MessageTypeCallMissed   MessageType = "call_missed"
	MessageTypeCallEnded    MessageType = "call_ended"
)

// IsCallEvent reports whether the type is a call-lifecycle marker. These (and
// the permission markers) are excluded from AI/analysis history so a "call
// received" log never reads as something the lead said.
func (t MessageType) IsCallEvent() bool {
	switch t {
	case MessageTypeCallReceived, MessageTypeCallAnswered, MessageTypeCallMissed, MessageTypeCallEnded,
		MessageTypeCallPermissionRequest, MessageTypeCallPermissionGranted, MessageTypeCallPermissionRejected:
		return true
	}
	return false
}

func InboundMessageTypes() []MessageType {
	return []MessageType{
		MessageTypeUserMessage,
		MessageTypeAudio,
		MessageTypeMedia,
	}
}

func InboundMessageTypeStrings() []string {
	types := InboundMessageTypes()
	result := make([]string, len(types))
	for i, t := range types {
		result[i] = string(t)
	}
	return result
}

func (t MessageType) IsInbound() bool {
	switch t {
	case MessageTypeUserMessage, MessageTypeAudio, MessageTypeMedia:
		return true
	}
	return false
}

func (t MessageType) Valid() bool {
	switch t {
	case MessageTypeUserMessage, MessageTypeAIResponse, MessageTypeToolCall, MessageTypeToolResult, MessageTypeAudio, MessageTypeSystem, MessageTypeMedia, MessageTypeOperator, MessageTypeTemplate:
		return true
	}
	return false
}

type MediaType string

const (
	MediaTypeImage    MediaType = "image"
	MediaTypeVideo    MediaType = "video"
	MediaTypeAudio    MediaType = "audio"
	MediaTypeDocument MediaType = "document"
	MediaTypeSticker  MediaType = "sticker"
)

func (t MediaType) Valid() bool {
	switch t {
	case MediaTypeImage, MediaTypeVideo, MediaTypeAudio, MediaTypeDocument, MediaTypeSticker:
		return true
	}
	return false
}

type DeliveryStatus string

const (
	DeliveryStatusNone      DeliveryStatus = ""
	DeliveryStatusSent      DeliveryStatus = "sent"
	DeliveryStatusDelivered DeliveryStatus = "delivered"
	DeliveryStatusRead      DeliveryStatus = "read"
	DeliveryStatusFailed    DeliveryStatus = "failed"
)

type Message struct {
	ID          string           `json:"id"`
	EntryID     string           `json:"entryId"`
	EntryType   shared.EntryType `json:"entryType"`
	Channel     MessageChannel   `json:"channel"`
	MessageType MessageType      `json:"messageType"`
	From        string           `json:"from"`
	To          string           `json:"to"`
	Text        string           `json:"text"`
	Image       []byte           `json:"image,omitempty"`
	Video       []byte           `json:"video,omitempty"`
	MediaID     *string          `json:"mediaId,omitempty"`
	MediaType   MediaType        `json:"mediaType,omitempty"`
	Read        bool             `json:"read"`
	ReadAt      *time.Time       `json:"readAt,omitempty"`
	ReadBy      *string          `json:"readBy,omitempty"`

	WhatsAppMessageID *string `json:"whatsappMessageId,omitempty" bson:"whatsappMessageId,omitempty"`

	ReplyToMessageID *string `json:"replyToMessageId,omitempty" bson:"replyToMessageId,omitempty"`

	DeliveryStatus DeliveryStatus  `json:"deliveryStatus,omitempty" bson:"deliveryStatus,omitempty"`
	SenderName     string          `json:"senderName,omitempty"`
	SenderAvatar   string          `json:"senderAvatar,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

func (m *Message) Normalize() {
	if m == nil {
		return
	}
	m.ID = strings.TrimSpace(m.ID)
	m.EntryID = strings.TrimSpace(m.EntryID)
	m.From = strings.TrimSpace(m.From)
	m.To = strings.TrimSpace(m.To)
	m.Text = strings.TrimSpace(m.Text)
}

func (m *Message) Validate() error {
	if m == nil {
		return ErrMessageContentRequired
	}
	if m.EntryID == "" {
		return ErrEntryIDRequired
	}
	if !m.EntryType.Valid() {
		return ErrEntryTypeInvalid
	}
	if m.ID == "" {
		return ErrMessageIDRequired
	}
	if m.From == "" && m.To == "" {
		return ErrMessageParticipantRequired
	}
	if m.Text == "" && len(m.Image) == 0 && len(m.Video) == 0 && m.MediaID == nil {
		return ErrMessageContentRequired
	}
	return nil
}

func (m *Message) IsFromUser() bool {
	return m.MessageType == MessageTypeUserMessage || m.MessageType == MessageTypeAudio
}

func (m *Message) HasMedia() bool {
	return m.MediaID != nil && *m.MediaID != ""
}
