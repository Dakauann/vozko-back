package metamessaging

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"
)

type EventKind string

const (
	EventInboundMessage    EventKind = "inbound_message"
	EventEchoMessage       EventKind = "echo_message"
	EventDeletedMessage    EventKind = "deleted_message"
	EventEditedMessage     EventKind = "edited_message"
	EventReaction          EventKind = "reaction"
	EventRead              EventKind = "read"
	EventDelivery          EventKind = "delivery"
	EventPostback          EventKind = "postback"
	EventReferral          EventKind = "referral"
	EventStandby           EventKind = "standby"
	EventThreadControl     EventKind = "thread_control"
	EventPolicyEnforcement EventKind = "policy_enforcement"
	EventOptin             EventKind = "optin"
	EventUnknown           EventKind = "unknown"
)

type Dialect struct {
	Prefix          string
	ClassifyStandby bool
}

type Event struct {
	Kind              EventKind
	AccountExternalID string
	ContactExternalID string
	Timestamp         time.Time
	Standby           bool

	IdempotencyKey string

	Message       *Message
	Reaction      *Reaction
	Read          *Read
	Delivery      *Delivery
	Postback      *Postback
	Referral      *Referral
	Edit          *MessageEdit
	ThreadControl *ThreadControl
	Policy        *PolicyEnforcement
	Optin         *Optin

	RawField string
	RawValue json.RawMessage
}

func (e *Event) IsOutbound() bool { return e.Kind == EventEchoMessage }

func (d Dialect) Key(account, kind, id string) string {
	return d.Prefix + ":" + account + ":" + kind + ":" + id
}

func NormalizeMessaging(env *EntryEnvelope, d Dialect) []*Event {
	if env == nil || env.Entry == nil {
		return nil
	}
	e := env.Entry
	events := make([]*Event, 0, len(e.Messaging)+len(e.Standby))
	for _, m := range e.Messaging {
		events = append(events, normalizeMessagingEvent(e, m, d, false)...)
	}
	for _, m := range e.Standby {
		events = append(events, normalizeMessagingEvent(e, m, d, true)...)
	}
	SortByTimestamp(events)
	return events
}

type timestamped interface {
	OccurredAt() time.Time
}

func (e *Event) OccurredAt() time.Time { return e.Timestamp }

func SortByTimestamp[T timestamped](events []T) {
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].OccurredAt().Before(events[j].OccurredAt())
	})
}

func normalizeMessagingEvent(entry *Entry, m *MessagingEvent, d Dialect, standby bool) []*Event {
	if m == nil {
		return nil
	}
	ts := UnixTime(m.Timestamp)
	if ts.IsZero() {
		ts = UnixTime(entry.Time)
	}
	account := entry.ID
	base := func(kind EventKind, key string) *Event {
		return &Event{
			Kind:              kind,
			AccountExternalID: account,
			ContactExternalID: m.Sender.ID,
			Timestamp:         ts,
			Standby:           standby,
			IdempotencyKey:    key,
		}
	}

	if standby && !d.ClassifyStandby {
		ev := base(EventStandby, d.Key(account, "standby", midOf(m)))
		ev.Message = m.Message
		return []*Event{ev}
	}

	switch {
	case m.Message != nil:
		msg := m.Message
		switch {
		case msg.Deleted():
			ev := base(EventDeletedMessage, d.Key(account, "deleted", msg.MID))
			ev.Message = msg
			return []*Event{ev}
		case msg.Echo():
			ev := base(EventEchoMessage, d.Key(account, "messages", msg.MID))
			ev.ContactExternalID = m.Recipient.ID
			ev.Message = msg
			return []*Event{ev}
		default:
			ev := base(EventInboundMessage, d.Key(account, "messages", msg.MID))
			if m.Recipient.ID != "" {
				ev.AccountExternalID = m.Recipient.ID
			}
			ev.Message = msg
			return []*Event{ev}
		}

	case m.MessageEdit != nil:
		ev := base(EventEditedMessage, d.Key(account, "edit", m.MessageEdit.MID+":"+m.MessageEdit.NumEdit.String()))
		ev.Edit = m.MessageEdit
		return []*Event{ev}

	case m.Reaction != nil:
		ev := base(EventReaction, d.Key(account, "reaction", m.Reaction.MID+":"+m.Reaction.Action+":"+m.Sender.ID))
		ev.Reaction = m.Reaction
		return []*Event{ev}

	case m.Read != nil:
		id := m.Read.MID
		if id == "" {
			id = "w" + strconv.FormatInt(m.Read.Watermark, 10)
		}
		ev := base(EventRead, d.Key(account, "read", id))
		ev.Read = m.Read
		return []*Event{ev}

	case m.Delivery != nil:
		ev := base(EventDelivery, d.Key(account, "delivery", "w"+strconv.FormatInt(m.Delivery.Watermark, 10)))
		ev.Delivery = m.Delivery
		return []*Event{ev}

	case m.Postback != nil:
		id := m.Postback.MID
		if id == "" {
			id = m.Postback.Payload + ":" + fmt.Sprint(m.Timestamp)
		}
		ev := base(EventPostback, d.Key(account, "postback", id))
		if m.Recipient.ID != "" {
			ev.AccountExternalID = m.Recipient.ID
		}
		ev.Postback = m.Postback
		return []*Event{ev}

	case m.Referral != nil:
		ev := base(EventReferral, d.Key(account, "referral", m.Referral.Ref+":"+fmt.Sprint(m.Timestamp)))
		if m.Recipient.ID != "" {
			ev.AccountExternalID = m.Recipient.ID
		}
		ev.Referral = m.Referral
		return []*Event{ev}

	case m.PassThreadControl != nil, m.TakeThreadControl != nil:
		tc, verb := m.PassThreadControl, "pass"
		if tc == nil {
			tc, verb = m.TakeThreadControl, "take"
		}
		ev := base(EventThreadControl, d.Key(account, "thread", verb+":"+tc.NewOwnerAppID.String()+":"+m.Sender.ID+":"+fmt.Sprint(m.Timestamp)))
		ev.ThreadControl = tc
		return []*Event{ev}

	case m.PolicyEnforcement != nil:
		ev := base(EventPolicyEnforcement, d.Key(account, "policy", m.PolicyEnforcement.Action+":"+fmt.Sprint(m.Timestamp)))
		if m.Recipient.ID != "" {
			ev.AccountExternalID = m.Recipient.ID
		}
		ev.Policy = m.PolicyEnforcement
		return []*Event{ev}

	case m.Optin != nil:
		ev := base(EventOptin, d.Key(account, "optin", m.Optin.Type+":"+m.Sender.ID+":"+fmt.Sprint(m.Timestamp)))
		ev.Optin = m.Optin
		return []*Event{ev}
	}
	return nil
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

func midOf(m *MessagingEvent) string {
	if m == nil || m.Message == nil {
		return ""
	}
	return m.Message.MID
}

func (d Dialect) EntryDedupKey(env *EntryEnvelope) string {
	if env == nil || env.Entry == nil {
		return ""
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return d.Prefix + ":entry:" + env.Entry.ID + ":" + hex.EncodeToString(sum[:])
}
