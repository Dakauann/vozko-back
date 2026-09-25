package unofficial_whatsapp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidEvent = errors.New("unofficial whatsapp: webhook body is not a recognisable event")
	ErrNoInstance   = errors.New("unofficial whatsapp: webhook body names no instance")
)

type EventKind string

const (
	EventInboundMessage     EventKind = "inbound_message"
	EventOutboundEcho       EventKind = "outbound_echo"
	EventOutboundFromDevice EventKind = "outbound_from_device"

	EventMessageStatus  EventKind = "message_status"
	EventMessageEdited  EventKind = "message_edited"
	EventMessageDeleted EventKind = "message_deleted"
	EventReaction       EventKind = "reaction"

	EventConnection    EventKind = "connection"
	EventContactUpdate EventKind = "contact_update"
	EventBlockToggle   EventKind = "block_toggle"
	EventCall          EventKind = "call"
	EventGroupChanged  EventKind = "group_changed"

	EventIgnored EventKind = "ignored"
	EventUnknown EventKind = "unknown"
)

type MediaKind string

const (
	MediaNone     MediaKind = ""
	MediaImage    MediaKind = "image"
	MediaVideo    MediaKind = "video"
	MediaAudio    MediaKind = "audio"
	MediaVoice    MediaKind = "voice"
	MediaDocument MediaKind = "document"
	MediaSticker  MediaKind = "sticker"
)

func (k MediaKind) CanAttach() bool {
	switch k {
	case MediaImage, MediaVideo, MediaAudio, MediaVoice, MediaDocument, MediaSticker:
		return true
	default:
		return false
	}
}

const (
	providerEventMessages       = "messages"
	providerEventMessagesUpdate = "messages_update"
	providerEventConnection     = "connection"
	providerEventHistory        = "history"
	providerEventChats          = "chats"
	providerEventContacts       = "contacts"
	providerEventBlocks         = "blocks"
	providerEventCall           = "call"
	providerEventPresence       = "presence"
	providerEventNewsletter     = "newsletter_messages"
	providerEventGroups         = "groups"
)

const TrackSource = "vozko"

type Envelope struct {
	Event    string          `json:"event"`
	Instance string          `json:"instance"`
	Owner    string          `json:"owner"`
	Data     json.RawMessage `json:"data"`
}

type Event struct {
	Kind          EventKind
	FromMe        bool
	ProviderEvent string
	InstanceID    string

	IdempotencyKey string

	ChatID  string
	IsGroup bool

	SenderJID   string
	SenderLID   string
	SenderPhone string
	SenderName  string

	ProviderMessageID string
	TrackID           string

	Text                    string
	QuotedProviderMessageID string
	OptionID                string

	Media    MediaKind
	MIMEType string
	FileName string

	PictureURL string

	Emoji                   string
	TargetProviderMessageID string

	DeliveryStatus DeliveryStatus

	Blocked      bool
	SessionState string

	Timestamp time.Time

	Backfill bool

	Raw json.RawMessage
}

type DeliveryStatus string

const (
	DeliveryUnknown   DeliveryStatus = ""
	DeliveryQueued    DeliveryStatus = "queued"
	DeliverySent      DeliveryStatus = "sent"
	DeliveryDelivered DeliveryStatus = "delivered"
	DeliveryRead      DeliveryStatus = "read"
	DeliveryFailed    DeliveryStatus = "failed"
	DeliveryDeleted   DeliveryStatus = "deleted"
)

func (e *Event) Outbound() bool {
	return e.Kind == EventOutboundEcho || e.Kind == EventOutboundFromDevice
}

func (e *Event) RunsAutomation() bool {
	return e.Kind == EventInboundMessage && !e.Backfill
}

func (e *Event) SubjectJID() string {
	if e.IsGroup {
		return e.ChatID
	}
	if e.SenderJID != "" {
		return e.SenderJID
	}
	return e.ChatID
}

type providerMessage struct {
	ID                  string       `json:"id"`
	MessageID           string       `json:"messageid"`
	ChatID              string       `json:"chatid"`
	Sender              string       `json:"sender"`
	SenderPN            string       `json:"sender_pn"`
	SenderLID           string       `json:"sender_lid"`
	SenderName          string       `json:"senderName"`
	IsGroup             bool         `json:"isGroup"`
	FromMe              bool         `json:"fromMe"`
	MessageType         string       `json:"messageType"`
	MessageTimestamp    int64        `json:"messageTimestamp"`
	Status              string       `json:"status"`
	Text                string       `json:"text"`
	Quoted              string       `json:"quoted"`
	Reaction            string       `json:"reaction"`
	Vote                string       `json:"vote"`
	ButtonOrListID      string       `json:"buttonOrListid"`
	ButtonOrListText    string       `json:"buttonOrListText"`
	SelectedDisplayText string       `json:"selectedDisplayText"`
	Title               string       `json:"title"`
	Content             contentField `json:"content"`
	Edited              string       `json:"edited"`
	WasSentByAPI        bool         `json:"wasSentByApi"`
	TrackSource         string       `json:"track_source"`
	TrackID             string       `json:"track_id"`
	MIMEType            string       `json:"mimetype"`
	FileName            string       `json:"fileName"`
	FileURL             string       `json:"fileURL"`
}

func DecodeEnvelope(body []byte) (*Envelope, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, ErrInvalidEvent
	}

	env := Envelope{
		Event:    lowerString(rawString(raw, "EventType", "eventType", "event_type", "event", "type")),
		Instance: rawString(raw, "instance", "instanceId", "instance_id", "instanceID"),
		Owner:    rawString(raw, "owner"),
	}
	if env.Event == "" {
		return nil, ErrInvalidEvent
	}

	env.Data = firstRaw(raw, "data", env.Event, singular(env.Event))
	if len(env.Data) == 0 {
		env.Data = body
	}
	return &env, nil
}

func DescribeUnknownBody(body []byte) []string {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return []string{"<not a json object>"}
	}
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func rawString(raw map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		var s string
		if v, ok := raw[key]; ok && json.Unmarshal(v, &s) == nil {
			if trimmed := strings.TrimSpace(s); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func firstRaw(raw map[string]json.RawMessage, keys ...string) json.RawMessage {
	for _, key := range keys {
		if key == "" {
			continue
		}
		if v, ok := raw[key]; ok && len(v) > 0 && string(v) != "null" {
			return v
		}
	}
	return nil
}

func lowerString(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func singular(event string) string {
	if strings.HasSuffix(event, "s") {
		return strings.TrimSuffix(event, "s")
	}
	return ""
}

func NormalizeEnvelope(instanceID string, env *Envelope) []*Event {
	if env == nil {
		return nil
	}

	switch env.Event {
	case providerEventMessages, providerEventMessagesUpdate, providerEventHistory:
		messages := decodeMessages(env.Data)
		out := make([]*Event, 0, len(messages))
		for i := range messages {
			if ev := normalizeMessage(instanceID, env, &messages[i]); ev != nil {
				out = append(out, ev)
			}
		}
		return out

	case providerEventConnection:
		return []*Event{normalizeConnection(instanceID, env)}

	case providerEventBlocks:
		return []*Event{normalizeBlock(instanceID, env)}

	case providerEventContacts, providerEventChats:
		return []*Event{normalizeContact(instanceID, env)}

	case providerEventGroups:
		return []*Event{normalizeGroupChange(instanceID, env)}

	case providerEventCall:
		return []*Event{{
			Kind: EventCall, ProviderEvent: env.Event, InstanceID: instanceID,
			ChatID:         stringField(env.Data, "chatid", "from", "id"),
			Timestamp:      time.Now().UTC(),
			IdempotencyKey: hashKey(instanceID, env.Event, env.Data),
			Raw:            env.Data,
		}}

	case providerEventPresence, providerEventNewsletter:
		return []*Event{{
			Kind: EventIgnored, ProviderEvent: env.Event,
			InstanceID: instanceID, Raw: env.Data,
		}}
	}

	return []*Event{{
		Kind: EventUnknown, ProviderEvent: env.Event,
		InstanceID:     instanceID,
		IdempotencyKey: hashKey(instanceID, env.Event, env.Data),
		Raw:            env.Data,
	}}
}

type contentField struct {
	Text string
}

func (c *contentField) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	switch b[0] {
	case '"':
		var s string
		if err := json.Unmarshal(b, &s); err == nil {
			c.Text = s
		}
	case '{':
		var obj struct {
			Text    string `json:"text"`
			Caption string `json:"caption"`
			Title   string `json:"title"`
		}
		if err := json.Unmarshal(b, &obj); err == nil {
			c.Text = firstNonEmptyString(obj.Text, obj.Caption, obj.Title)
		}
	}
	return nil
}

func (c contentField) String() string { return c.Text }

func decodeMessages(data json.RawMessage) []providerMessage {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var many []providerMessage
		if err := json.Unmarshal(data, &many); err != nil {
			return nil
		}
		return many
	}
	var one providerMessage
	if err := json.Unmarshal(data, &one); err != nil {
		return nil
	}
	return []providerMessage{one}
}

func normalizeMessage(instanceID string, env *Envelope, msg *providerMessage) *Event {
	chatID := strings.TrimSpace(msg.ChatID)
	if IsNewsletterJID(chatID) {
		return nil
	}

	providerID := firstNonEmptyString(msg.MessageID, msg.ID)
	ev := &Event{
		ProviderEvent:           env.Event,
		InstanceID:              instanceID,
		ChatID:                  chatID,
		IsGroup:                 msg.IsGroup || IsGroupJID(chatID),
		ProviderMessageID:       providerID,
		TrackID:                 strings.TrimSpace(msg.TrackID),
		Text:                    msg.Text,
		QuotedProviderMessageID: strings.TrimSpace(msg.Quoted),
		OptionID:                firstNonEmptyString(msg.ButtonOrListID, msg.Vote),
		SenderName:              strings.TrimSpace(msg.SenderName),
		DeliveryStatus:          normalizeDeliveryStatus(msg.Status),
		Timestamp:               timestampFromMillis(msg.MessageTimestamp),
		Backfill:                env.Event == providerEventHistory,
		MIMEType:                strings.TrimSpace(msg.MIMEType),
		FileName:                strings.TrimSpace(msg.FileName),
		Raw:                     env.Data,
	}
	if strings.TrimSpace(ev.Text) == "" && ev.OptionID != "" {
		ev.Text = firstNonEmptyString(
			msg.ButtonOrListText, msg.SelectedDisplayText, msg.Title, msg.Content.Text, ev.OptionID)
	}

	if strings.TrimSpace(ev.Text) == "" {
		ev.Text = msg.Content.Text
	}

	ev.Media = mediaKindFor(msg.MessageType)
	resolveSenderIdentity(ev, msg)
	ev.IdempotencyKey = messageIdempotencyKey(instanceID, env.Event, providerID, ev)
	ev.Kind = classifyMessage(env, msg, ev)
	ev.FromMe = msg.FromMe

	if ev.Kind == EventReaction {
		ev.Emoji = msg.Text
		ev.TargetProviderMessageID = strings.TrimSpace(msg.Reaction)
	}
	if env.Event == providerEventMessagesUpdate {
		ev.TargetProviderMessageID = providerID
	}
	return ev
}

func classifyMessage(env *Envelope, msg *providerMessage, ev *Event) EventKind {
	if env.Event == providerEventMessagesUpdate {
		switch {
		case ev.DeliveryStatus == DeliveryDeleted:
			return EventMessageDeleted
		case strings.TrimSpace(msg.Edited) != "":
			return EventMessageEdited
		default:
			return EventMessageStatus
		}
	}

	if strings.TrimSpace(msg.Reaction) != "" || msg.MessageType == "reaction" {
		return EventReaction
	}

	if !msg.FromMe {
		return EventInboundMessage
	}
	if ours := strings.EqualFold(msg.TrackSource, TrackSource) && ev.TrackID != ""; ours {
		return EventOutboundEcho
	}
	return EventOutboundFromDevice
}

func resolveSenderIdentity(ev *Event, msg *providerMessage) {
	contactRef := strings.TrimSpace(msg.Sender)
	if msg.FromMe || contactRef == "" {
		contactRef = ev.ChatID
	}

	if strings.HasSuffix(contactRef, "@"+DomainLID) {
		ev.SenderLID = contactRef
	} else {
		ev.SenderJID = contactRef
	}
	if pn := strings.TrimSpace(msg.SenderPN); pn != "" && !msg.FromMe {
		ev.SenderJID = pn
	}
	if lid := strings.TrimSpace(msg.SenderLID); lid != "" && !msg.FromMe {
		ev.SenderLID = lid
	}

	if ev.SenderJID == "" && ev.SenderLID == "" {
		ev.SenderJID = ev.ChatID
	}
	ev.SenderPhone = PhoneFromJID(ev.SenderJID)
	if ev.SenderPhone == "" {
		ev.SenderPhone = PhoneFromJID(ev.ChatID)
	}
}

func normalizeConnection(instanceID string, env *Envelope) *Event {
	return &Event{
		Kind:           EventConnection,
		ProviderEvent:  env.Event,
		InstanceID:     instanceID,
		SessionState:   stringField(env.Data, "status", "state", "connection"),
		Timestamp:      time.Now().UTC(),
		IdempotencyKey: hashKey(instanceID, env.Event, env.Data),
		Raw:            env.Data,
	}
}

func normalizeBlock(instanceID string, env *Envelope) *Event {
	return &Event{
		Kind:           EventBlockToggle,
		ProviderEvent:  env.Event,
		InstanceID:     instanceID,
		ChatID:         stringField(env.Data, "chatid", "jid", "id"),
		Blocked:        boolField(env.Data, "isBlocked", "blocked"),
		Timestamp:      time.Now().UTC(),
		IdempotencyKey: hashKey(instanceID, env.Event, env.Data),
		Raw:            env.Data,
	}
}

func normalizeGroupChange(instanceID string, env *Envelope) *Event {
	chatID := stringField(env.Data, "groupjid", "groupJID", "JID", "jid", "chatid", "id")
	return &Event{
		Kind:          EventGroupChanged,
		ProviderEvent: env.Event,
		InstanceID:    instanceID,
		ChatID:        chatID,
		IsGroup:       true,
		Timestamp:     time.Now().UTC(),
		Raw:           env.Data,
	}
}

func normalizeContact(instanceID string, env *Envelope) *Event {
	chatID := stringField(env.Data, "wa_chatid", "chatid", "jid", "id")
	return &Event{
		Kind:          EventContactUpdate,
		ProviderEvent: env.Event,
		InstanceID:    instanceID,
		ChatID:        chatID,
		SenderJID:     chatID,
		SenderPhone:   PhoneFromJID(chatID),
		SenderName:    stringField(env.Data, "wa_contactName", "wa_name", "name", "verifiedName"),
		PictureURL:    stringField(env.Data, "imagePreview", "image", "profilePicUrl"),
		IsGroup:       IsGroupJID(chatID),
		Timestamp:     time.Now().UTC(),
		Raw:           env.Data,
	}
}

func messageIdempotencyKey(instanceID, providerEvent, providerID string, ev *Event) string {
	if providerID == "" {
		return hashKey(instanceID, providerEvent, ev.Raw)
	}
	key := "uw:" + instanceID + ":" + providerEvent + ":" + providerID
	if ev.DeliveryStatus != DeliveryUnknown && providerEvent == providerEventMessagesUpdate {
		key += ":" + string(ev.DeliveryStatus)
	}
	return key
}

func normalizeDeliveryStatus(raw string) DeliveryStatus {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "queued", "pending":
		return DeliveryQueued
	case "sent":
		return DeliverySent
	case "delivered":
		return DeliveryDelivered
	case "read":
		return DeliveryRead
	case "failed", "canceled", "cancelled", "error":
		return DeliveryFailed
	case "deleted":
		return DeliveryDeleted
	}
	return DeliveryUnknown
}

func mediaKindFor(messageType string) MediaKind {
	switch strings.ToLower(strings.TrimSpace(messageType)) {
	case "image", "imagemessage":
		return MediaImage
	case "video", "videomessage", "videoplay", "ptv":
		return MediaVideo
	case "audio", "audiomessage":
		return MediaAudio
	case "ptt", "myaudio", "voice":
		return MediaVoice
	case "document", "documentmessage":
		return MediaDocument
	case "sticker", "stickermessage":
		return MediaSticker
	}
	return MediaNone
}

func timestampFromMillis(millis int64) time.Time {
	if millis <= 0 {
		return time.Now().UTC()
	}
	if millis < 1_000_000_000_000 {
		return time.Unix(millis, 0).UTC()
	}
	return time.UnixMilli(millis).UTC()
}

func stringField(data json.RawMessage, keys ...string) string {
	var raw map[string]any
	if json.Unmarshal(data, &raw) != nil {
		return ""
	}
	for _, key := range keys {
		switch v := raw[key].(type) {
		case string:
			if trimmed := strings.TrimSpace(v); trimmed != "" {
				return trimmed
			}
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64)
		}
	}
	return ""
}

func boolField(data json.RawMessage, keys ...string) bool {
	var raw map[string]any
	if json.Unmarshal(data, &raw) != nil {
		return false
	}
	for _, key := range keys {
		if v, ok := raw[key].(bool); ok {
			return v
		}
	}
	return false
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func hashKey(instanceID, providerEvent string, payload []byte) string {
	sum := sha256.Sum256(payload)
	return "uw:" + instanceID + ":" + providerEvent + ":" + hex.EncodeToString(sum[:16])
}
