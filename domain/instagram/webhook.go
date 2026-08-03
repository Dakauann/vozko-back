package instagram

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

var ErrInvalidWebhookPayload = errors.New("instagram: invalid webhook payload")

// ---------------------------------------------------------------- raw payload

// Envelope is the top level of an Instagram webhook POST.
//
// Meta ships this as a bare object on some doc pages and as a single-element
// ARRAY on others, so DecodeEnvelope sniffs and accepts both.
type Envelope struct {
	Object string   `json:"object"`
	Entry  []*Entry `json:"entry"`
}

// Entry carries FOUR mutually-exclusive shapes on the same endpoint:
//
//  1. Messaging, DM events
//  2. Standby  , messages on the standby channel (we don't own thread control)
//  3. Changes  , Facebook-Login style change events, wrapped in an array
//  4. Field/Value directly on the entry, Instagram-Login style change events,
//     with NO changes array. Missing this shape is the single most common
//     Instagram integration bug.
//
// A single POST may hold up to 1000 entries and may span multiple Instagram
// accounts, so callers must iterate and route each entry by ID.
type Entry struct {
	ID   string `json:"id"`
	Time int64  `json:"time"`

	Messaging []*MessagingEvent `json:"messaging,omitempty"`
	Standby   []*MessagingEvent `json:"standby,omitempty"`
	Changes   []*Change         `json:"changes,omitempty"`

	// Shape 4: field/value hoisted onto the entry itself.
	Field string          `json:"field,omitempty"`
	Value json.RawMessage `json:"value,omitempty"`
}

type Change struct {
	Field string          `json:"field"`
	Value json.RawMessage `json:"value"`
}

// MessagingEvent is one DM-family event. Exactly one of the payload fields is
// set; note that Reaction, Read, Postback, Referral and MessageEdit are SIBLINGS
// of Message, not nested inside it.
type MessagingEvent struct {
	Sender    Participant `json:"sender"`
	Recipient Participant `json:"recipient"`
	Timestamp int64       `json:"timestamp"`

	Message     *Message     `json:"message,omitempty"`
	Reaction    *Reaction    `json:"reaction,omitempty"`
	Read        *Read        `json:"read,omitempty"`
	Postback    *Postback    `json:"postback,omitempty"`
	Referral    *Referral    `json:"referral,omitempty"`
	MessageEdit *MessageEdit `json:"message_edit,omitempty"`
}

type Participant struct {
	ID string `json:"id"`
}

// Message is a DM.
//
// IsEcho / IsDeleted / IsSelf / IsUnsupported are PRESENCE-ONLY: Meta includes
// them solely when true, so they must be pointers. A plain bool with omitempty
// would silently lose the distinction between absent and false.
type Message struct {
	MID           string        `json:"mid"`
	Text          string        `json:"text,omitempty"`
	Attachments   []*Attachment `json:"attachments,omitempty"`
	IsEcho        *bool         `json:"is_echo,omitempty"`
	IsDeleted     *bool         `json:"is_deleted,omitempty"`
	IsSelf        *bool         `json:"is_self,omitempty"`
	IsUnsupported *bool         `json:"is_unsupported,omitempty"`
	QuickReply    *QuickReply   `json:"quick_reply,omitempty"`
	Referral      *Referral     `json:"referral,omitempty"`
	ReplyTo       *ReplyTo      `json:"reply_to,omitempty"`
}

// ReplyTo is a UNION: MID for an inline reply to a message, Story for a reply
// to one of our stories. Branch on which is present.
type ReplyTo struct {
	MID         string `json:"mid,omitempty"`
	IsSelfReply *bool  `json:"is_self_reply,omitempty"`
	Story       *Story `json:"story,omitempty"`
}

type Story struct {
	ID             string `json:"id"`
	URL            string `json:"url"`
	LinkStickerURL string `json:"link_sticker_url,omitempty"`
}

// Attachment payload types: audio, file, image, share, story_mention, video,
// ig_reel, reel, ephemeral. Note that `ephemeral` carries NO payload at all, so
// Payload must be nil-checked before every dereference.
type Attachment struct {
	Type    string             `json:"type"`
	Payload *AttachmentPayload `json:"payload,omitempty"`
}

type AttachmentPayload struct {
	URL         string `json:"url,omitempty"`
	Title       string `json:"title,omitempty"`
	ID          string `json:"id,omitempty"`
	ReelVideoID string `json:"reel_video_id,omitempty"`
}

type QuickReply struct {
	Payload string `json:"payload"`
}

// Reaction arrives as a sibling of Message. Action is react|unreact; on unreact
// both Reaction and Emoji may be absent. Reaction is NOT a closed enum, the
// docs list different sets and explicitly allow "other", so the raw string is
// preserved rather than mapped.
type Reaction struct {
	MID      string `json:"mid"`
	Action   string `json:"action"`
	Reaction string `json:"reaction,omitempty"`
	Emoji    string `json:"emoji,omitempty"`
}

// Read is Instagram's read receipt. It carries a specific message id, there is
// NO watermark, so we cannot infer "everything before T is read".
type Read struct {
	MID string `json:"mid"`
}

type Postback struct {
	MID     string `json:"mid"`
	Title   string `json:"title"`
	Payload string `json:"payload"`
}

type Referral struct {
	Ref    string `json:"ref"`
	Source string `json:"source"`
	Type   string `json:"type"`
	AdID   string `json:"ad_id,omitempty"`
}

// MessageEdit carries an edited DM. NumEdit is documented with a STRING
// placeholder, so it is decoded permissively.
type MessageEdit struct {
	MID     string      `json:"mid"`
	Text    string      `json:"text"`
	NumEdit json.Number `json:"num_edit,omitempty"`
}

// CommentValue is a comments/live_comments change value.
//
// The comment id key DIFFERS by login type: `id` under Instagram Login, and
// `comment_id` under Facebook Login. Both are accepted and resolved by
// CommentID().
type CommentValue struct {
	ID        string        `json:"id,omitempty"`
	CommentID string        `json:"comment_id,omitempty"`
	ParentID  string        `json:"parent_id,omitempty"`
	Text      string        `json:"text"`
	From      *CommentFrom  `json:"from,omitempty"`
	Media     *CommentMedia `json:"media,omitempty"`
}

func (c *CommentValue) ResolvedCommentID() string {
	if c.ID != "" {
		return c.ID
	}
	return c.CommentID
}

type CommentFrom struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type CommentMedia struct {
	ID               string `json:"id"`
	MediaProductType string `json:"media_product_type,omitempty"`
}

// ---------------------------------------------------------------- decoding

// DecodeEnvelope parses a webhook body, tolerating both the bare-object and
// array-wrapped top-level forms.
func DecodeEnvelope(body []byte) ([]*Envelope, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, ErrInvalidWebhookPayload
	}

	switch trimmed[0] {
	case '[':
		var envs []*Envelope
		if err := json.Unmarshal(trimmed, &envs); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidWebhookPayload, err)
		}
		return envs, nil
	case '{':
		var env Envelope
		if err := json.Unmarshal(trimmed, &env); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidWebhookPayload, err)
		}
		return []*Envelope{&env}, nil
	default:
		return nil, ErrInvalidWebhookPayload
	}
}

// SplitEntries flattens every envelope into individual entries paired with the
// account they belong to.
//
// Splitting at ingest gives per-account failure isolation: one tenant's poison
// event cannot block another's, and a 1000-entry batch spanning many accounts
// becomes many independently retryable queue messages.
func SplitEntries(envelopes []*Envelope) []*EntryEnvelope {
	out := make([]*EntryEnvelope, 0, len(envelopes))
	for _, env := range envelopes {
		if env == nil {
			continue
		}
		for _, e := range env.Entry {
			if e == nil {
				continue
			}
			out = append(out, &EntryEnvelope{Object: env.Object, Entry: e})
		}
	}
	return out
}

// EntryEnvelope is one entry addressed to one Instagram account, the unit we
// publish to the queue.
type EntryEnvelope struct {
	Object string `json:"object"`
	Entry  *Entry `json:"entry"`
}

// ---------------------------------------------------------------- normalized

// EventKind classifies a webhook event after normalization, so consumers switch
// on an explicit kind instead of re-sniffing raw JSON.
type EventKind string

const (
	EventInboundMessage EventKind = "inbound_message"
	EventEchoMessage    EventKind = "echo_message"
	EventDeletedMessage EventKind = "deleted_message"
	EventEditedMessage  EventKind = "edited_message"
	EventReaction       EventKind = "reaction"
	EventRead           EventKind = "read"
	EventPostback       EventKind = "postback"
	EventReferral       EventKind = "referral"
	EventStandby        EventKind = "standby"
	EventComment        EventKind = "comment"
	EventLiveComment    EventKind = "live_comment"
	EventUnknown        EventKind = "unknown"
)

// Event is a normalized webhook event. One raw entry can yield several.
type Event struct {
	Kind EventKind
	// IGAccountExternalID is the business account this event belongs to.
	IGAccountExternalID string
	// ContactIGSID is the other party. For change events it is the commenter.
	ContactIGSID string
	// Timestamp is derived from the event, not from arrival.
	Timestamp time.Time

	// IdempotencyKey uniquely identifies this event. One `mid` legitimately
	// recurs across kinds (original, tombstone, edit, read, reaction), so the
	// key is composite.
	IdempotencyKey string

	Message  *Message
	Reaction *Reaction
	Read     *Read
	Postback *Postback
	Referral *Referral
	Edit     *MessageEdit
	Comment  *CommentValue

	// RawField / RawValue carry unrecognised change events so they can be
	// logged verbatim rather than dropped. Three documented fields have no
	// published payload shape, so guessing would be wrong.
	RawField string
	RawValue json.RawMessage
}

// IsOutbound reports whether this event describes a message we sent.
func (e *Event) IsOutbound() bool { return e.Kind == EventEchoMessage }

// NormalizeEntry converts one raw entry into normalized events, ordered by
// event timestamp.
//
// Meta gives no ordering guarantee and explicitly directs implementers to order
// by the webhook timestamp field, so we sort here rather than trusting arrival
// order.
func NormalizeEntry(env *EntryEnvelope) []*Event {
	if env == nil || env.Entry == nil {
		return nil
	}
	e := env.Entry
	events := make([]*Event, 0, len(e.Messaging)+len(e.Standby)+len(e.Changes)+1)

	for _, m := range e.Messaging {
		if ev := normalizeMessaging(e, m, false); ev != nil {
			events = append(events, ev...)
		}
	}
	for _, m := range e.Standby {
		if ev := normalizeMessaging(e, m, true); ev != nil {
			events = append(events, ev...)
		}
	}
	for _, c := range e.Changes {
		if c == nil {
			continue
		}
		if ev := normalizeChange(e, c.Field, c.Value); ev != nil {
			events = append(events, ev)
		}
	}
	// Shape 4, field/value directly on the entry (Instagram Login).
	if e.Field != "" {
		if ev := normalizeChange(e, e.Field, e.Value); ev != nil {
			events = append(events, ev)
		}
	}

	sort.SliceStable(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})
	return events
}

func normalizeMessaging(entry *Entry, m *MessagingEvent, standby bool) []*Event {
	if m == nil {
		return nil
	}
	ts := millisToTime(m.Timestamp)
	if ts.IsZero() {
		ts = millisToTime(entry.Time)
	}

	// The business is identified by recipient.id for inbound traffic. Meta's own
	// examples set entry[].id to the SENDER's scoped id for postback and
	// referral events, so entry[].id is not a reliable account discriminator.
	account := entry.ID
	contact := m.Sender.ID

	base := func(kind EventKind, key string) *Event {
		return &Event{
			Kind:                kind,
			IGAccountExternalID: account,
			ContactIGSID:        contact,
			Timestamp:           ts,
			IdempotencyKey:      key,
		}
	}

	if standby {
		ev := base(EventStandby, idemKey(account, "standby", midOf(m)))
		ev.Message = m.Message
		return []*Event{ev}
	}

	switch {
	case m.Message != nil:
		msg := m.Message
		switch {
		case boolVal(msg.IsDeleted):
			ev := base(EventDeletedMessage, idemKey(account, "deleted", msg.MID))
			ev.Message = msg
			return []*Event{ev}

		case boolVal(msg.IsEcho):
			// On an echo the roles are reversed: sender is the business.
			ev := base(EventEchoMessage, idemKey(account, "messages", msg.MID))
			ev.ContactIGSID = m.Recipient.ID
			ev.Message = msg
			return []*Event{ev}

		default:
			ev := base(EventInboundMessage, idemKey(account, "messages", msg.MID))
			// Prefer recipient.id as the business identifier where it disagrees
			// with entry.id.
			if m.Recipient.ID != "" {
				ev.IGAccountExternalID = m.Recipient.ID
			}
			ev.Message = msg
			return []*Event{ev}
		}

	case m.MessageEdit != nil:
		ev := base(EventEditedMessage, idemKey(account, "edit", m.MessageEdit.MID+":"+m.MessageEdit.NumEdit.String()))
		ev.Edit = m.MessageEdit
		return []*Event{ev}

	case m.Reaction != nil:
		key := idemKey(account, "reaction", m.Reaction.MID+":"+m.Reaction.Action+":"+m.Sender.ID)
		ev := base(EventReaction, key)
		ev.Reaction = m.Reaction
		return []*Event{ev}

	case m.Read != nil:
		ev := base(EventRead, idemKey(account, "read", m.Read.MID))
		ev.Read = m.Read
		return []*Event{ev}

	case m.Postback != nil:
		ev := base(EventPostback, idemKey(account, "postback", m.Postback.MID))
		if m.Recipient.ID != "" {
			ev.IGAccountExternalID = m.Recipient.ID
		}
		ev.Postback = m.Postback
		return []*Event{ev}

	case m.Referral != nil:
		ev := base(EventReferral, idemKey(account, "referral", m.Referral.Ref+":"+fmt.Sprint(m.Timestamp)))
		if m.Recipient.ID != "" {
			ev.IGAccountExternalID = m.Recipient.ID
		}
		ev.Referral = m.Referral
		return []*Event{ev}
	}

	return nil
}

func normalizeChange(entry *Entry, field string, value json.RawMessage) *Event {
	ts := millisToTime(entry.Time)

	switch field {
	case "comments", "live_comments":
		var cv CommentValue
		if len(value) > 0 {
			if err := json.Unmarshal(value, &cv); err != nil {
				// Malformed value: keep it as unknown so it is logged, not lost.
				return &Event{
					Kind:                EventUnknown,
					IGAccountExternalID: entry.ID,
					Timestamp:           ts,
					IdempotencyKey:      idemKey(entry.ID, "unknown:"+field, fmt.Sprint(entry.Time)),
					RawField:            field,
					RawValue:            value,
				}
			}
		}
		kind := EventComment
		if field == "live_comments" {
			kind = EventLiveComment
		}
		ev := &Event{
			Kind:                kind,
			IGAccountExternalID: entry.ID,
			Timestamp:           ts,
			IdempotencyKey:      idemKey(entry.ID, "comment", cv.ResolvedCommentID()),
			Comment:             &cv,
		}
		if cv.From != nil {
			ev.ContactIGSID = cv.From.ID
		}
		return ev

	default:
		// Unknown or undocumented field. Never drop it silently: three
		// subscribable fields have no published payload shape.
		return &Event{
			Kind:                EventUnknown,
			IGAccountExternalID: entry.ID,
			Timestamp:           ts,
			IdempotencyKey:      idemKey(entry.ID, "unknown:"+field, fmt.Sprint(entry.Time)),
			RawField:            field,
			RawValue:            value,
		}
	}
}

// MediaKindForAttachment maps an Instagram attachment type onto our media
// vocabulary. Unknown types return "" so callers can record them as unsupported
// rather than guessing.
func MediaKindForAttachment(t string) string {
	switch t {
	case "image":
		return "image"
	case "video", "ig_reel", "reel":
		return "video"
	case "audio":
		return "audio"
	case "file":
		return "document"
	}
	return ""
}

func idemKey(account, kind, id string) string {
	return "ig:" + account + ":" + kind + ":" + id
}

func midOf(m *MessagingEvent) string {
	if m == nil || m.Message == nil {
		return ""
	}
	return m.Message.MID
}

func boolVal(b *bool) bool { return b != nil && *b }

func millisToTime(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}
