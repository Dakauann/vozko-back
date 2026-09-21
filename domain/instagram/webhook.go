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

type Envelope struct {
	Object string   `json:"object"`
	Entry  []*Entry `json:"entry"`
}

type Entry struct {
	ID   string `json:"id"`
	Time int64  `json:"time"`

	Messaging []*MessagingEvent `json:"messaging,omitempty"`
	Standby   []*MessagingEvent `json:"standby,omitempty"`
	Changes   []*Change         `json:"changes,omitempty"`

	Field string          `json:"field,omitempty"`
	Value json.RawMessage `json:"value,omitempty"`
}

type Change struct {
	Field string          `json:"field"`
	Value json.RawMessage `json:"value"`
}

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

type Reaction struct {
	MID      string `json:"mid"`
	Action   string `json:"action"`
	Reaction string `json:"reaction,omitempty"`
	Emoji    string `json:"emoji,omitempty"`
}

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

type MessageEdit struct {
	MID     string      `json:"mid"`
	Text    string      `json:"text"`
	NumEdit json.Number `json:"num_edit,omitempty"`
}

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

type EntryEnvelope struct {
	Object string `json:"object"`
	Entry  *Entry `json:"entry"`
}

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

type Event struct {
	Kind                EventKind
	IGAccountExternalID string
	ContactIGSID        string
	Timestamp           time.Time

	IdempotencyKey string

	Message  *Message
	Reaction *Reaction
	Read     *Read
	Postback *Postback
	Referral *Referral
	Edit     *MessageEdit
	Comment  *CommentValue

	RawField string
	RawValue json.RawMessage
}

func (e *Event) IsOutbound() bool { return e.Kind == EventEchoMessage }

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
	ts := unixTimestampToTime(m.Timestamp)
	if ts.IsZero() {
		ts = unixTimestampToTime(entry.Time)
	}

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
			ev := base(EventEchoMessage, idemKey(account, "messages", msg.MID))
			ev.ContactIGSID = m.Recipient.ID
			ev.Message = msg
			return []*Event{ev}

		default:
			ev := base(EventInboundMessage, idemKey(account, "messages", msg.MID))
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
	ts := unixTimestampToTime(entry.Time)

	switch field {
	case "comments", "live_comments":
		var cv CommentValue
		if len(value) > 0 {
			if err := json.Unmarshal(value, &cv); err != nil {
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

func unixTimestampToTime(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	if value < 100_000_000_000 {
		return time.Unix(value, 0).UTC()
	}
	return time.UnixMilli(value).UTC()
}
