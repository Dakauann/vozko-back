package conversation

import (
	"encoding/json"
	"strings"
	"time"

	"vozko/domain/shared"
)

type MessageChannel string

const (
	MessageChannelWhatsApp           MessageChannel = "whatsapp"
	MessageChannelInstagram          MessageChannel = "instagram"
	MessageChannelTelegram           MessageChannel = "telegram"
	MessageChannelUnofficialWhatsApp MessageChannel = "unofficial_whatsapp"
)

func (c MessageChannel) Valid() bool {
	switch c {
	case MessageChannelWhatsApp,
		MessageChannelInstagram, MessageChannelTelegram, MessageChannelUnofficialWhatsApp:
		return true
	}
	return false
}

type MessageTransport string

const (
	MessageTransportAPI         MessageTransport = ""
	MessageTransportBusinessApp MessageTransport = "business_app"
)

func (t MessageTransport) IsMetaBillable() bool {
	return t != MessageTransportBusinessApp
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

	MessageTypeCallReceived MessageType = "call_received"
	MessageTypeCallAnswered MessageType = "call_answered"
	MessageTypeCallMissed   MessageType = "call_missed"
	MessageTypeCallEnded    MessageType = "call_ended"

	MessageTypeStoryReply   MessageType = "story_reply"
	MessageTypeStoryMention MessageType = "story_mention"
	MessageTypeReaction     MessageType = "reaction"
	MessageTypeUnsupported  MessageType = "unsupported"
	MessageTypePostShare    MessageType = "post_share"
)

const (
	SeedMetadataKey      = "seed"
	SeedSourceLeadImport = "lead_import"
)

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

func (t MessageType) IsMetaServiceBillable() bool {
	switch t {
	case MessageTypeOperator, MessageTypeAIResponse, MessageTypeMedia, MessageTypeAudio:
		return true
	}
	return false
}

func AllMessageTypes() []MessageType {
	return []MessageType{
		MessageTypeUserMessage,
		MessageTypeAIResponse,
		MessageTypeToolCall,
		MessageTypeToolResult,
		MessageTypeAudio,
		MessageTypeSystem,
		MessageTypeMedia,
		MessageTypeOperator,
		MessageTypeTemplate,
		MessageTypeCallPermissionRequest,
		MessageTypeCallPermissionGranted,
		MessageTypeCallPermissionRejected,
		MessageTypeCallReceived,
		MessageTypeCallAnswered,
		MessageTypeCallMissed,
		MessageTypeCallEnded,
		MessageTypeStoryReply,
		MessageTypeStoryMention,
		MessageTypeReaction,
		MessageTypeUnsupported,
		MessageTypePostShare,
	}
}

func ServiceMessageTypes() []MessageType {
	return []MessageType{
		MessageTypeOperator,
		MessageTypeAIResponse,
		MessageTypeMedia,
		MessageTypeAudio,
	}
}

func ServiceMessageTypeStrings() []string {
	types := ServiceMessageTypes()
	result := make([]string, len(types))
	for i, t := range types {
		result[i] = string(t)
	}
	return result
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

func (s DeliveryStatus) IsMetaBillable() bool {
	switch s {
	case DeliveryStatusSent, DeliveryStatusDelivered, DeliveryStatusRead:
		return true
	}
	return false
}

func BillableDeliveryStatuses() []DeliveryStatus {
	return []DeliveryStatus{
		DeliveryStatusSent,
		DeliveryStatusDelivered,
		DeliveryStatusRead,
	}
}

func BillableDeliveryStatusStrings() []string {
	statuses := BillableDeliveryStatuses()
	result := make([]string, len(statuses))
	for i, s := range statuses {
		result[i] = string(s)
	}
	return result
}

type Message struct {
	ID          string                  `json:"id"`
	EntryID     string                  `json:"entryId"`
	EntryType   shared.EntryType        `json:"entryType"`
	Channel     MessageChannel          `json:"channel"`
	MessageType MessageType             `json:"messageType"`
	Direction   MessageHistoryDirection `json:"direction,omitempty"`
	From        string                  `json:"from"`
	To          string                  `json:"to"`
	Text        string                  `json:"text"`
	Image       []byte                  `json:"image,omitempty"`
	Video       []byte                  `json:"video,omitempty"`
	MediaID     *string                 `json:"mediaId,omitempty"`
	MediaType   MediaType               `json:"mediaType,omitempty"`
	Read        bool                    `json:"read"`
	ReadAt      *time.Time              `json:"readAt,omitempty"`
	ReadBy      *string                 `json:"readBy,omitempty"`

	WhatsAppMessageID *string `json:"whatsappMessageId,omitempty" bson:"whatsappMessageId,omitempty"`

	ExternalMessageID *string `json:"externalMessageId,omitempty"`

	ReplyToMessageID *string `json:"replyToMessageId,omitempty" bson:"replyToMessageId,omitempty"`

	DeliveryStatus DeliveryStatus   `json:"deliveryStatus,omitempty" bson:"deliveryStatus,omitempty"`
	SenderName     string           `json:"senderName,omitempty"`
	SenderAvatar   string           `json:"senderAvatar,omitempty"`
	SentVia        MessageTransport `json:"sentVia,omitempty"`
	SentBy         SentBy           `json:"sentBy"`
	Metadata       json.RawMessage  `json:"metadata,omitempty"`
	CreatedAt      time.Time        `json:"createdAt"`
	UpdatedAt      time.Time        `json:"updatedAt"`
}

func (m *Message) ResolvedDirection() MessageHistoryDirection {
	if m == nil {
		return MessageDirectionUnknown
	}
	if m.SentBy.Valid() {
		return m.SentBy.Direction()
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

	m.MediaID = nilIfBlank(m.MediaID)
	m.ReadBy = nilIfBlank(m.ReadBy)
}

func nilIfBlank(v *string) *string {
	if v == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*v)
	if trimmed == "" {
		return nil
	}
	if trimmed == *v {
		return v
	}
	return &trimmed
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
	if !m.SentBy.Valid() {
		return ErrMessageSenderRequired
	}
	return nil
}

func (m *Message) IsFromUser() bool {
	return m.MessageType == MessageTypeUserMessage || m.MessageType == MessageTypeAudio
}

func (m *Message) FromCustomer() bool {
	return m != nil && !m.ResolvedDirection().IsOutbound()
}

func (m *Message) Transcribable() bool {
	if m == nil {
		return false
	}
	switch m.MessageType {
	case MessageTypeToolCall, MessageTypeToolResult, MessageTypeSystem:
		return false
	}
	if m.MessageType.IsCallEvent() {
		return false
	}
	return strings.TrimSpace(m.Text) != ""
}

func (m *Message) HasMedia() bool {
	return m.MediaID != nil && *m.MediaID != ""
}
