package instagram

import (
	"encoding/json"
	"fmt"

	mm "vozko/domain/metamessaging"
)

var ErrInvalidWebhookPayload = mm.ErrInvalidWebhookPayload

var webhookDialect = mm.Dialect{Prefix: "ig"}

const (
	EventComment     mm.EventKind = "comment"
	EventLiveComment mm.EventKind = "live_comment"
)

type Event struct {
	mm.Event
	Comment *CommentValue
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

func NormalizeEntry(env *mm.EntryEnvelope) []*Event {
	if env == nil || env.Entry == nil {
		return nil
	}
	e := env.Entry
	messaging := mm.NormalizeMessaging(env, webhookDialect)
	events := make([]*Event, 0, len(messaging)+len(e.Changes)+1)
	for _, ev := range messaging {
		events = append(events, &Event{Event: *ev})
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
	mm.SortByTimestamp(events)
	return events
}

func normalizeChange(entry *mm.Entry, field string, value json.RawMessage) *Event {
	ts := mm.UnixTime(entry.Time)
	unknown := func() *Event {
		return &Event{Event: mm.Event{
			Kind:              mm.EventUnknown,
			AccountExternalID: entry.ID,
			Timestamp:         ts,
			IdempotencyKey:    webhookDialect.Key(entry.ID, "unknown:"+field, fmt.Sprint(entry.Time)),
			RawField:          field,
			RawValue:          value,
		}}
	}

	switch field {
	case "comments", "live_comments":
		var cv CommentValue
		if len(value) > 0 {
			if err := json.Unmarshal(value, &cv); err != nil {
				return unknown()
			}
		}
		kind := EventComment
		if field == "live_comments" {
			kind = EventLiveComment
		}
		ev := &Event{
			Event: mm.Event{
				Kind:              kind,
				AccountExternalID: entry.ID,
				Timestamp:         ts,
				IdempotencyKey:    webhookDialect.Key(entry.ID, "comment", cv.ResolvedCommentID()),
			},
			Comment: &cv,
		}
		if cv.From != nil {
			ev.ContactExternalID = cv.From.ID
		}
		return ev
	default:
		return unknown()
	}
}

func EntryDedupKey(env *mm.EntryEnvelope) string { return webhookDialect.EntryDedupKey(env) }
