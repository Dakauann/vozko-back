package conversation

import (
	"encoding/json"
	"strings"
	"time"

	"vozko/domain/shared"
)

type MessageChannel string

const (
	MessageChannelVoice     MessageChannel = "voice"
	MessageChannelWhatsApp  MessageChannel = "whatsapp"
	MessageChannelSupport   MessageChannel = "support"
	MessageChannelInstagram MessageChannel = "instagram"
	MessageChannelTelegram  MessageChannel = "telegram"
	// MessageChannelUnofficialWhatsApp is WhatsApp over a linked-device session. It is
	// distinct from MessageChannelWhatsApp because the transport, not the
	// channel, is what a message row has to record: the two have different
	// provider ids, different delivery semantics and different send paths, and a
	// report grouped by channel must be able to tell them apart.
	MessageChannelUnofficialWhatsApp MessageChannel = "unofficial_whatsapp"
)

func (c MessageChannel) Valid() bool {
	switch c {
	case MessageChannelVoice, MessageChannelWhatsApp, MessageChannelSupport,
		MessageChannelInstagram, MessageChannelTelegram, MessageChannelUnofficialWhatsApp:
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
	// phone call log. These are event/system markers, never conversational turns
	// (see IsCallEvent / AI history exclusion) so they don't pollute AI context.
	MessageTypeCallReceived MessageType = "call_received"
	MessageTypeCallAnswered MessageType = "call_answered"
	MessageTypeCallMissed   MessageType = "call_missed"
	MessageTypeCallEnded    MessageType = "call_ended"

	// Instagram-specific inbound shapes. A story reply and a story mention are
	// conversational turns and carry the story context in Metadata; a reaction
	// and an unsupported message are markers.
	MessageTypeStoryReply   MessageType = "story_reply"
	MessageTypeStoryMention MessageType = "story_mention"
	MessageTypeReaction     MessageType = "reaction"
	MessageTypeUnsupported  MessageType = "unsupported"
	MessageTypePostShare    MessageType = "post_share"
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
		// Instagram story replies and mentions are genuine inbound turns, so
		// they must count toward unread and appear in AI history. Channels that
		// cannot produce them are unaffected.
		MessageTypeStoryReply,
		MessageTypeStoryMention,
		MessageTypePostShare,
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
	case MessageTypeUserMessage, MessageTypeAudio, MessageTypeMedia,
		MessageTypeStoryReply, MessageTypeStoryMention, MessageTypePostShare:
		return true
	}
	return false
}

func (t MessageType) Valid() bool {
	switch t {
	case MessageTypeUserMessage, MessageTypeAIResponse, MessageTypeToolCall, MessageTypeToolResult, MessageTypeAudio, MessageTypeSystem, MessageTypeMedia, MessageTypeOperator, MessageTypeTemplate,
		MessageTypeStoryReply, MessageTypeStoryMention, MessageTypeReaction, MessageTypeUnsupported, MessageTypePostShare:
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
	// Direction is who sent this, independent of what it contained.
	//
	// Empty on rows written before the column existed; readers must treat that
	// as "not stated" and fall back, never as inbound. See
	// MessageHistoryDirection for why this is stored rather than derived.
	Direction MessageHistoryDirection `json:"direction,omitempty"`
	From      string                  `json:"from"`
	To        string                  `json:"to"`
	Text      string                  `json:"text"`
	Image     []byte                  `json:"image,omitempty"`
	Video     []byte                  `json:"video,omitempty"`
	MediaID   *string                 `json:"mediaId,omitempty"`
	MediaType MediaType               `json:"mediaType,omitempty"`
	Read      bool                    `json:"read"`
	ReadAt    *time.Time              `json:"readAt,omitempty"`
	ReadBy    *string                 `json:"readBy,omitempty"`

	WhatsAppMessageID *string `json:"whatsappMessageId,omitempty" bson:"whatsappMessageId,omitempty"`

	// ExternalMessageID is the provider's message id for any channel. WhatsApp
	// still writes WhatsAppMessageID (40+ call sites depend on it); channels
	// added from Instagram onward use this field, which is covered by a partial
	// UNIQUE index on (entry_type, external_message_id) so duplicate webhook
	// deliveries are rejected by the database and not only by the Redis guard.
	ExternalMessageID *string `json:"externalMessageId,omitempty"`

	ReplyToMessageID *string `json:"replyToMessageId,omitempty" bson:"replyToMessageId,omitempty"`

	DeliveryStatus DeliveryStatus  `json:"deliveryStatus,omitempty" bson:"deliveryStatus,omitempty"`
	SenderName     string          `json:"senderName,omitempty"`
	SenderAvatar   string          `json:"senderAvatar,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

// ResolvedDirection is the direction to persist for this message.
//
// A stated direction always wins. When none was stated — the direct
// messageRepo.Create paths, which never took one — it is derived from the
// message type, which is exactly the old inference and therefore exactly the old
// behaviour for those callers. That derivation is sound for them: they are the
// official WhatsApp and coexistence paths, where an operator reply really is
// MessageTypeOperator and a customer's really is MessageTypeUserMessage.
//
// It is NOT sound for a channel that names its content honestly, which is the
// whole reason direction is now stored: an owner answering on their own
// WhatsApp sends a message whose content type is a plain text one, and deriving
// from that puts their reply on the customer's side of the thread. Those
// channels go through MessageHistoryManager, which states it.
func (m *Message) ResolvedDirection() MessageHistoryDirection {
	if m == nil {
		return MessageDirectionUnknown
	}
	if m.Direction.Valid() {
		return m.Direction
	}
	if m.MessageType.IsInbound() {
		return MessageDirectionInbound
	}
	return MessageDirectionOutbound
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
