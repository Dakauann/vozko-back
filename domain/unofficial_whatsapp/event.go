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

// The inbound normalizer: one provider webhook body becomes a flat, ordered list
// of events the rest of the system can reason about without knowing the vendor.
//
// The provider declares its webhook envelope as `{event, instance, data}` with
// `data` typed only as a map, so this decoder is deliberately TOLERANT: unknown
// event kinds are classified rather than dropped, absent fields fall back rather
// than failing, and anything unrecognised keeps its raw payload so it can be
// logged and investigated. A strict decoder here would mean a vendor change
// silently discarding a customer's messages.

var (
	ErrInvalidEvent = errors.New("unofficial whatsapp: webhook body is not a recognisable event")
	ErrNoInstance   = errors.New("unofficial whatsapp: webhook body names no instance")
)

// EventKind is what happened, resolved from the provider's event name plus the
// shape of its payload.
type EventKind string

const (
	// EventInboundMessage is a message from the contact.
	EventInboundMessage EventKind = "inbound_message"
	// EventOutboundEcho is our own send coming back. It carries the correlation
	// id we stamped, so it reconciles against the row we already wrote.
	EventOutboundEcho EventKind = "outbound_echo"
	// EventOutboundFromDevice is a message the OWNER typed on their own phone.
	//
	// Distinct from an echo and genuinely valuable: it is the operator
	// answering from the handset, and it belongs in the CRM transcript. It must
	// never trigger automation, or the AI answers its own colleague.
	EventOutboundFromDevice EventKind = "outbound_from_device"

	EventMessageStatus  EventKind = "message_status"
	EventMessageEdited  EventKind = "message_edited"
	EventMessageDeleted EventKind = "message_deleted"
	EventReaction       EventKind = "reaction"

	EventConnection    EventKind = "connection"
	EventContactUpdate EventKind = "contact_update"
	EventBlockToggle   EventKind = "block_toggle"
	EventCall          EventKind = "call"
	// EventGroupChanged says that something about a group changed, and
	// deliberately does not say what.
	//
	// The provider documents the payload only as "a map, the shape varies", so
	// parsing a rename or a membership delta out of it would be guessing — and a
	// guess that is wrong writes a roster nobody can tell is wrong. The event is
	// consumed as an invalidation: mark the cached group stale, re-read from the
	// authoritative endpoint. That costs one call and cannot be subtly incorrect.
	EventGroupChanged EventKind = "group_changed"

	// EventIgnored is a real event we deliberately do not act on: a newsletter
	// post, a presence tick. Classified rather than dropped so the volume is
	// visible in metrics.
	EventIgnored EventKind = "ignored"
	// EventUnknown is an event kind the vendor added. Logged raw, never
	// discarded: this provider ships new event types without notice.
	EventUnknown EventKind = "unknown"
)

// MediaKind is the normalized attachment type.
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

// Provider event names, as they arrive in the envelope.
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

// TrackSource is stamped on every message we send, so the echo is recognisable.
//
// It is our own marker rather than a provider feature: the vendor treats
// track_source/track_id as free-form correlation fields, which is exactly what
// makes them usable as an echo tag.
const TrackSource = "vozko"

// Envelope is the provider's webhook body.
type Envelope struct {
	Event    string          `json:"event"`
	Instance string          `json:"instance"`
	Owner    string          `json:"owner"`
	Data     json.RawMessage `json:"data"`
}

// Event is one normalized occurrence.
type Event struct {
	Kind EventKind
	// ProviderEvent is the vendor's own event name, kept for logging and metrics
	// so an unknown kind is investigable.
	ProviderEvent string
	InstanceID    string

	// IdempotencyKey deduplicates a redelivery. Never empty for an event that
	// mutates state.
	IdempotencyKey string

	// ChatID is the conversation address (a JID).
	ChatID  string
	IsGroup bool

	// Contact identity. Both JID forms are carried because WhatsApp addresses
	// the same human by either, and treating them as two people splits one real
	// chat into two CRM conversations.
	SenderJID   string
	SenderLID   string
	SenderPhone string
	SenderName  string

	// ProviderMessageID is the vendor's message id, stored as
	// external_message_id so a redelivery updates instead of inserting.
	ProviderMessageID string
	// TrackID is the correlation id we stamped on our own send. Present only on
	// an echo, and the thing that makes an echo recognisable.
	TrackID string

	Text string
	// QuotedProviderMessageID is the message this one replies to.
	QuotedProviderMessageID string
	// OptionID is the machine-readable id of a tapped button or selected list
	// row. Workflows branch on this, never on the visible label.
	OptionID string

	Media    MediaKind
	MIMEType string
	FileName string

	// PictureURL is the PROVIDER's avatar url, when an event volunteered one.
	//
	// The `chats` and `contacts` events carry the vendor's Chat object, which
	// includes the picture — so a profile-photo change arrives as a push and
	// costs no provider call at all. It is a source url, never something to
	// store: the bytes are re-hosted first.
	PictureURL string

	// Emoji is the reaction body; empty on a reaction REMOVAL, which is a
	// meaningful state rather than a missing field.
	Emoji string
	// TargetProviderMessageID is what a reaction or a status update refers to.
	TargetProviderMessageID string

	// DeliveryStatus is the provider's lifecycle value, normalized.
	DeliveryStatus DeliveryStatus

	// Blocked carries a block toggle's new state.
	Blocked bool
	// SessionState is the raw connection state, mapped by the caller.
	SessionState string

	Timestamp time.Time

	// Backfill marks an event replayed from history on connect.
	//
	// Load-bearing: a connect replays up to seven days at once, and running
	// assignment, AI replies, workflow triggers and unread counts over that
	// burst would bury an operator under conversations nobody has triaged. The
	// transcript is still persisted in full.
	Backfill bool

	// Raw is the untouched payload, kept so an unknown kind can be logged.
	Raw json.RawMessage
}

// DeliveryStatus is the normalized message lifecycle.
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

// Outbound reports whether this event describes a message we or the owner sent.
func (e *Event) Outbound() bool {
	return e.Kind == EventOutboundEcho || e.Kind == EventOutboundFromDevice
}

// RunsAutomation reports whether this event may trigger attendance at all.
//
// Only a genuine inbound message from a live (non-backfilled) delivery
// qualifies. Every other answer here is a loop or a burst waiting to happen: an
// echo would have the AI answer itself, an owner's phone message would have it
// answer a colleague, and a backfill would have it answer seven days of history
// all at once.
//
// It deliberately says NOTHING about groups. It used to, and that was the bug:
// an event cannot know whether its instance opted into group attendance, so the
// hard exclusion here ran before Instance.HandleGroups was ever consulted and
// made that setting unreachable. The group decision belongs to
// Conversation.InScope, which has both facts.
func (e *Event) RunsAutomation() bool {
	return e.Kind == EventInboundMessage && !e.Backfill
}

// SubjectJID is the identity of the chat this event belongs to.
//
// For a private chat that is the person; for a group it is the GROUP, not the
// participant who happened to speak. Resolving the subject from the sender is
// what turned one group thread into one CRM conversation per member, each
// labelled with a random participant's name.
func (e *Event) SubjectJID() string {
	if e.IsGroup {
		return e.ChatID
	}
	if e.SenderJID != "" {
		return e.SenderJID
	}
	return e.ChatID
}

// providerMessage is the vendor's stored message object, narrowed to what we
// consume. Field names are the vendor's.
type providerMessage struct {
	ID          string `json:"id"`
	MessageID   string `json:"messageid"`
	ChatID      string `json:"chatid"`
	Sender      string `json:"sender"`
	SenderPN    string `json:"sender_pn"`
	SenderLID   string `json:"sender_lid"`
	SenderName  string `json:"senderName"`
	IsGroup     bool   `json:"isGroup"`
	FromMe      bool   `json:"fromMe"`
	MessageType string `json:"messageType"`
	// Timestamp is MILLISECONDS. The other channels in this codebase carry
	// seconds, and a missed division puts every message in the year 57000 and
	// destroys inbox ordering silently.
	MessageTimestamp int64  `json:"messageTimestamp"`
	Status           string `json:"status"`
	Text             string `json:"text"`
	Quoted           string `json:"quoted"`
	Reaction         string `json:"reaction"`
	Vote             string `json:"vote"`
	ButtonOrListID   string `json:"buttonOrListid"`
	// The VISIBLE label of a tapped button or selected list row. The vendor
	// spells it differently depending on the interactive type, and when the
	// message is a button press `text` is empty — so without these the
	// transcript records that the customer sent nothing at all.
	ButtonOrListText    string `json:"buttonOrListText"`
	SelectedDisplayText string `json:"selectedDisplayText"`
	Title               string `json:"title"`
	// Content is polymorphic in the vendor's payload, which is why it has its own
	// type. See contentField: declaring it as a plain string silently discarded
	// every message whose content arrived as an object.
	Content      contentField `json:"content"`
	Edited       string       `json:"edited"`
	WasSentByAPI bool         `json:"wasSentByApi"`
	TrackSource  string       `json:"track_source"`
	TrackID      string       `json:"track_id"`
	MIMEType     string       `json:"mimetype"`
	FileName     string       `json:"fileName"`
	FileURL      string       `json:"fileURL"`
}

// DecodeEnvelope parses the outer webhook body.
func DecodeEnvelope(body []byte) (*Envelope, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, ErrInvalidEvent
	}

	env := Envelope{
		// The provider does not spell the event key the same way everywhere:
		// `EventType` is what a live instance sends, while `event` appears in
		// the docs and in older payloads. Decoding into a struct could not
		// bridge that — encoding/json matches names case-insensitively but not
		// across DIFFERENT names — and getting it wrong drops every inbound
		// message with nothing but "unrecognisable" to show for it.
		Event:    lowerString(rawString(raw, "EventType", "eventType", "event_type", "event", "type")),
		Instance: rawString(raw, "instance", "instanceId", "instance_id", "instanceID"),
		Owner:    rawString(raw, "owner"),
	}
	if env.Event == "" {
		return nil, ErrInvalidEvent
	}

	// Where the payload lives also varies: `data` in the documented shape, a
	// key named after the event ("message", "chat") on a live instance, and
	// occasionally the top level itself. Falling back to the whole body is safe
	// because every reader looks fields up by name, so a flat payload still
	// resolves; the alternative is discarding an event we could have read.
	env.Data = firstRaw(raw, "data", env.Event, singular(env.Event))
	if len(env.Data) == 0 {
		env.Data = body
	}
	return &env, nil
}

// DescribeUnknownBody names the top-level keys of a body we could not decode.
//
// KEYS ONLY, deliberately. A dropped body is usually a real customer message,
// and logging values would put message text and phone numbers into the log sink
// — the precise data this channel exists to protect. The key names alone are
// enough to identify a shape change, which is the only reason to look.
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

// rawString returns the first key that holds a non-empty JSON string.
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

// firstRaw returns the first key that holds a non-null value.
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

// lowerString normalizes an event name, because the same event arrives as
// "messages" and "Messages" depending on which surface emitted it.
func lowerString(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// singular maps a plural event name to the singular key its payload often uses
// ("messages" -> "message"), which is where a live instance puts the object.
func singular(event string) string {
	if strings.HasSuffix(event, "s") {
		return strings.TrimSuffix(event, "s")
	}
	return ""
}

// NormalizeEnvelope turns one webhook body into zero or more events.
//
// A slice rather than a single event because the provider batches: `history`
// replays many messages at once, and `messages` has been observed carrying
// either an object or an array. Accepting both costs one branch and avoids
// discarding a whole batch on a shape we did not expect.
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
		// Real events we deliberately do not act on. Classified rather than
		// dropped so their volume stays visible.
		return []*Event{{
			Kind: EventIgnored, ProviderEvent: env.Event,
			InstanceID: instanceID, Raw: env.Data,
		}}
	}

	// A kind the vendor added. Never discarded: this provider ships new event
	// types without notice, and a silent drop is indistinguishable from a
	// working integration.
	return []*Event{{
		Kind: EventUnknown, ProviderEvent: env.Event,
		InstanceID:     instanceID,
		IdempotencyKey: hashKey(instanceID, env.Event, env.Data),
		Raw:            env.Data,
	}}
}

// contentField is the vendor's `content`, which is NOT one type.
//
// A plain text message sends it as a string ("bom dia"). Anything richer — an
// ExtendedTextMessage, which is what a reply, a quote or a message carrying a
// link becomes, and every media type — sends an OBJECT instead, with the words
// under `text` or `caption`.
//
// Declaring it `string` made encoding/json fail the whole struct on the object
// form, and decodeMessages turns any unmarshal failure into "no messages at
// all". So one field of the wrong type silently discarded entire conversations:
// group chatter survived because it is mostly plain text, while a direct thread
// where people quote and link each other vanished completely. No error, no log,
// nothing written.
//
// UnmarshalJSON therefore never returns an error. A shape we have not seen must
// cost the words in that one field, never the message: this provider has no
// replay endpoint, so a message dropped here is gone for good.
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

// decodeMessages accepts either a single message object or an array of them.
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

// normalizeMessage classifies one message and resolves its identity.
func normalizeMessage(instanceID string, env *Envelope, msg *providerMessage) *Event {
	chatID := strings.TrimSpace(msg.ChatID)
	// A newsletter post is a publishing surface, not an attendance surface, and
	// creating a conversation for one would put a broadcast channel in the
	// operator's queue.
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
	// An interactive reply says something even when `text` is empty: the customer
	// tapped a labelled button. Recording it as an empty message meant the
	// persistence layer rejected the whole event ("message content is required")
	// and the tap was retried three times and then dropped — the operator saw
	// their own question and no answer.
	//
	// The option id is the last resort rather than the first: it is a machine
	// identifier, but a transcript reading "sim_agendar" is still infinitely
	// better than one that lost the customer's reply.
	if strings.TrimSpace(ev.Text) == "" && ev.OptionID != "" {
		ev.Text = firstNonEmptyString(
			msg.ButtonOrListText, msg.SelectedDisplayText, msg.Title, msg.Content.Text, ev.OptionID)
	}

	ev.Media = mediaKindFor(msg.MessageType)
	resolveSenderIdentity(ev, msg)
	ev.IdempotencyKey = messageIdempotencyKey(instanceID, env.Event, providerID, ev)
	ev.Kind = classifyMessage(env, msg, ev)

	// A reaction names the message it applies to, not itself.
	if ev.Kind == EventReaction {
		ev.Emoji = msg.Text
		ev.TargetProviderMessageID = strings.TrimSpace(msg.Reaction)
	}
	if env.Event == providerEventMessagesUpdate {
		ev.TargetProviderMessageID = providerID
	}
	return ev
}

// classifyMessage decides what a message row actually represents.
//
// The three-way split on fromMe is the whole reason this channel can be honest
// about who said what:
//
//   - fromMe with OUR track id: our own send coming back. Reconcile, never
//     insert, and never re-enter automation — that is the loop the vendor's
//     docs warn about, closed here by recognition rather than by discarding the
//     event and losing delivery receipts with it.
//   - fromMe without it: the owner answered on their own phone. A real message
//     that belongs in the transcript, and the reason we do not filter API-sent
//     messages away at the provider.
//   - not fromMe: the contact wrote.
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
	// wasSentByApi without our track source means another integration sharing
	// this instance sent it. Still not ours to answer, and still part of the
	// transcript.
	return EventOutboundFromDevice
}

// resolveSenderIdentity fills every identifier form the payload offers for the
// AUTHOR — whoever actually spoke.
//
// The author is not the same thing as the conversation's subject, and conflating
// them is what split one group thread into one conversation per member. In a
// group the author is a participant while the subject is the group; in a private
// chat they are the same person. SubjectJID answers the other half.
//
// For an outbound message the "sender" is the connected account rather than the
// contact, so the CHAT identifies the other party — using the sender there would
// attribute the conversation to the business's own number.
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

	// A group message's author is a participant, but the CONVERSATION is the
	// group, so the chat id stays authoritative for addressing.
	if ev.SenderJID == "" && ev.SenderLID == "" {
		ev.SenderJID = ev.ChatID
	}
	ev.SenderPhone = PhoneFromJID(ev.SenderJID)
	if ev.SenderPhone == "" {
		// PhoneFromJID returns empty for a group id, so this fallback resolves a
		// private chat whose sender arrived only as a LID and never invents a
		// number out of "120363…@g.us".
		ev.SenderPhone = PhoneFromJID(ev.ChatID)
	}
}

func normalizeConnection(instanceID string, env *Envelope) *Event {
	return &Event{
		Kind:          EventConnection,
		ProviderEvent: env.Event,
		InstanceID:    instanceID,
		SessionState:  stringField(env.Data, "status", "state", "connection"),
		Timestamp:     time.Now().UTC(),
		// Connection events carry no id of their own, so the key is a digest of
		// the payload. Two identical states in a row are the same fact anyway.
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

// normalizeGroupChange turns a `groups` delivery into an invalidation.
//
// Only ONE field is read — which group — and everything else in the payload is
// deliberately ignored. The provider specifies no schema for this event, and a
// normalizer that guessed at "name changed to X" or "participant Y left" would
// write a roster that is confidently wrong, which is strictly worse than one
// that is briefly stale. The handler re-reads /group/info, which cannot be.
//
// The JID is looked for under every spelling the provider has been seen to use,
// for the same reason DecodeEnvelope does: there is no contract to rely on.
func normalizeGroupChange(instanceID string, env *Envelope) *Event {
	chatID := stringField(env.Data, "groupjid", "groupJID", "JID", "jid", "chatid", "id")
	return &Event{
		Kind:          EventGroupChanged,
		ProviderEvent: env.Event,
		InstanceID:    instanceID,
		ChatID:        chatID,
		IsGroup:       true,
		Timestamp:     time.Now().UTC(),
		// Not deduplicated by payload digest: two identical notifications are
		// two separate "something changed" facts, and suppressing the second
		// would drop a real change that happened to look like the first.
		Raw: env.Data,
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
		// The preview size first: it is what a 40px avatar needs, and the full
		// original is several hundred kilobytes for an identical result.
		PictureURL: stringField(env.Data, "imagePreview", "image", "profilePicUrl"),
		IsGroup:    IsGroupJID(chatID),
		Timestamp:  time.Now().UTC(),
		// Deliberately NOT deduplicated by payload digest: a profile refresh is
		// idempotent by nature, and suppressing a repeat would stop a renamed
		// contact from ever updating.
		Raw: env.Data,
	}
}

// ---------------------------------------------------------------- helpers

// messageIdempotencyKey scopes a provider message id to its instance and event.
//
// Scoped by instance because a message id is unique per account, not globally;
// scoped by event because a `messages` insert and a later `messages_update` for
// the same id are two distinct facts that must both be processed.
func messageIdempotencyKey(instanceID, providerEvent, providerID string, ev *Event) string {
	if providerID == "" {
		return hashKey(instanceID, providerEvent, ev.Raw)
	}
	key := "uw:" + instanceID + ":" + providerEvent + ":" + providerID
	// A status update repeats the same id for each step of the lifecycle, so
	// the status has to be part of the key or only the first one is processed
	// and the track never advances past Sent.
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

// mediaKindFor maps the vendor's message types onto our normalized kinds.
//
// `ptt` and `myaudio` are voice notes rather than plain audio, and the
// distinction is worth keeping: a voice note is what the speech-to-text path
// exists for, and rendering it as a music player is wrong.
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

// timestampFromMillis converts the vendor's millisecond clock.
//
// A zero or absurd value falls back to now: an out-of-range timestamp would
// otherwise sort the message to the beginning or the end of every inbox
// forever, which is worse than being a second late.
func timestampFromMillis(millis int64) time.Time {
	if millis <= 0 {
		return time.Now().UTC()
	}
	// Tolerate a provider that sends seconds despite documenting milliseconds:
	// anything below this bound cannot be a millisecond timestamp in this era.
	if millis < 1_000_000_000_000 {
		return time.Unix(millis, 0).UTC()
	}
	return time.UnixMilli(millis).UTC()
}

// stringField reads the first present string key, for payloads whose exact
// field names the vendor does not document.
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

// hashKey builds a stable idempotency key for an event with no natural id.
//
// A digest is the correct key for a payload the vendor gives no identifier
// for — two byte-identical bodies are the same fact — and it is far better than
// skipping dedup, which would let a redelivered connection event flap an
// instance's status.
func hashKey(instanceID, providerEvent string, payload []byte) string {
	sum := sha256.Sum256(payload)
	return "uw:" + instanceID + ":" + providerEvent + ":" + hex.EncodeToString(sum[:16])
}
