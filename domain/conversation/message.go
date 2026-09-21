package conversation

import (
	"encoding/json"
	"strings"
	"time"

	"vozko/domain/shared"
)

type MessageChannel string

const (
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
	case MessageChannelWhatsApp, MessageChannelSupport,
		MessageChannelInstagram, MessageChannelTelegram, MessageChannelUnofficialWhatsApp:
		return true
	}
	return false
}

// MessageTransport is HOW an outbound message left the building, which the
// channel cannot say on a coexistence number.
//
// Coexistence puts the WhatsApp Business app and the Cloud API on the same
// number. When the owner replies from the app on their phone, Meta echoes that
// message to our webhook and we store it exactly like one of our own: same
// channel, same entry type, same operator message type. Nothing distinguished
// the two.
//
// That distinction is money. Meta's rule is that messages sent from the
// WhatsApp Business app stay free and only messages sent through the API are
// billed, so counting an echo as a billable service message invents a cost that
// does not exist.
type MessageTransport string

const (
	// MessageTransportAPI is a message we sent through the Cloud API. It is the
	// empty string so every row written before this existed, and every channel
	// that has no second transport, reads as what it is.
	MessageTransportAPI MessageTransport = ""
	// MessageTransportBusinessApp is an echo of a message the business sent
	// from the WhatsApp Business app on a coexistence number. Free at Meta.
	MessageTransportBusinessApp MessageTransport = "business_app"
)

// IsMetaBillable reports whether Meta charges for a message that left over this
// transport. Only the API is billed.
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

// IsMetaServiceBillable reports whether a message of this type, sent outbound on
// the official WhatsApp channel, is one Meta charges us for as a SERVICE
// message from 1 October 2026.
//
// Meta's definition is "any non-template message that is not powered by Meta
// Business Agent", whether a person or a third-party AI produced it. Translated
// into our types that is an agent typing, our AI replying, and an agent
// attaching a file.
//
// What this deliberately excludes, because each one is stored OUTBOUND and
// would otherwise be counted as money we owe:
//   - tool_call / tool_result: AI internals that never left the building.
//   - the call lifecycle and permission markers: a phone log, not a message.
//   - template: already billed to the customer as a campaign send, and priced
//     by Meta on the template rate card rather than the service rate.
//   - the inbound-only types: Meta never charges for what arrives.
//
// Type alone is not the whole rule. A billable service message must also be
// outbound, on the official channel, and delivered; see IsMetaBillable for the
// delivery half.
func (t MessageType) IsMetaServiceBillable() bool {
	switch t {
	case MessageTypeOperator, MessageTypeAIResponse, MessageTypeMedia, MessageTypeAudio:
		return true
	}
	return false
}

// AllMessageTypes is every message type this package declares.
//
// It exists so that adding a type cannot silently change what we bill. The
// service message rule is an allowlist, so a new type it has not been told
// about is excluded by default, and excluding a billable type under-reports
// what Meta charges us with nothing failing anywhere.
//
// message_type_registry_test.go parses this file and fails if a declared
// MessageType constant is missing from this list, so the omission is caught at
// the moment the constant is added rather than at the next invoice.
//
// A sticker is deliberately not here: stickers are a MediaType carried by
// MessageTypeOperator or MessageTypeMedia, not a type of their own.
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

// ServiceMessageTypes is the single list behind every consumer of the service
// message rule: the reporting SQL, and the partial index that serves it.
//
// media and audio appear here and in InboundMessageTypes because they are the
// only genuinely two-way types: a customer sends a voice note, an agent attaches
// a receipt. Direction, not type, separates those two, which is why the
// reporting query filters on both.
//
// Adding a type here changes the partial index predicate, so it needs a
// migration to rebuild idx_cm_service_exposure, not only a deploy.
func ServiceMessageTypes() []MessageType {
	return []MessageType{
		MessageTypeOperator,
		MessageTypeAIResponse,
		MessageTypeMedia,
		MessageTypeAudio,
	}
}

// ServiceMessageTypeStrings renders ServiceMessageTypes for SQL, exactly as
// InboundMessageTypeStrings does for the unread rule. Both the IN list in the
// reporting query and the partial index predicate are built from this, so the
// index cannot drift away from the query it was created to serve.
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

// IsMetaBillable reports whether a message in this delivery state is one Meta
// charges for. Meta bills on delivery: "sent" already means it accepted the
// message, so all three forward states count.
//
// Failed does not, and neither does the empty status. Empty is what our own
// internal rows carry (tool_call, tool_result, the call markers) and also what
// rows written before the column existed carry. In both cases nothing says Meta
// ever saw the message, and the safe reading of "not stated" is not to pay for
// it.
func (s DeliveryStatus) IsMetaBillable() bool {
	switch s {
	case DeliveryStatusSent, DeliveryStatusDelivered, DeliveryStatusRead:
		return true
	}
	return false
}

// BillableDeliveryStatuses is the delivery half of the service message rule,
// kept beside ServiceMessageTypes so the reporting query builds both of its IN
// lists from the domain rather than from string literals in SQL.
func BillableDeliveryStatuses() []DeliveryStatus {
	return []DeliveryStatus{
		DeliveryStatusSent,
		DeliveryStatusDelivered,
		DeliveryStatusRead,
	}
}

// BillableDeliveryStatusStrings renders BillableDeliveryStatuses for SQL.
func BillableDeliveryStatusStrings() []string {
	statuses := BillableDeliveryStatuses()
	result := make([]string, len(statuses))
	for i, s := range statuses {
		result[i] = string(s)
	}
	return result
}

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
	// SentVia is how an outbound message left the building. Empty means the
	// Cloud API, which is every row written before coexistence echoes were
	// distinguishable and every channel that has only one transport.
	SentVia  MessageTransport `json:"sentVia,omitempty"`
	Metadata json.RawMessage  `json:"metadata,omitempty"`
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

	// MediaID and ReadBy back uuid columns, so an empty string is not a weaker
	// version of an id — it is a value Postgres refuses outright (22P02), and it
	// refuses the whole INSERT, not just the column. A send that reached the
	// provider then vanishes from the transcript: the customer has the message
	// and we have no record of it.
	//
	// Both fields are pointers precisely so "absent" is expressible, but callers
	// reach them through &someString and land on &"" whenever the source was
	// blank — an automation send with no operator, a media send with no
	// attachment. Collapsing that to nil here is the only fix that covers every
	// writer, since every path normalizes before it persists.
	m.MediaID = nilIfBlank(m.MediaID)
	m.ReadBy = nilIfBlank(m.ReadBy)
}

// nilIfBlank collapses a pointer to a blank string down to no pointer at all.
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
	return nil
}

func (m *Message) IsFromUser() bool {
	return m.MessageType == MessageTypeUserMessage || m.MessageType == MessageTypeAudio
}

func (m *Message) HasMedia() bool {
	return m.MediaID != nil && *m.MediaID != ""
}
