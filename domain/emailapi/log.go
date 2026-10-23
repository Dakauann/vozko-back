package emailapi

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/emailaddress"
)

const (
	DefaultPageSize = 50
	MaxPageSize     = 100
)

type Cursor struct {
	CreatedAt time.Time
	ID        string
}

func (c Cursor) Empty() bool { return c.ID == "" }

func (c Cursor) Encode() string {
	return base64.RawURLEncoding.EncodeToString([]byte(c.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + c.ID))
}

func ParseCursor(raw string) (Cursor, error) {
	if strings.TrimSpace(raw) == "" {
		return Cursor{}, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return Cursor{}, ErrCursorInvalid
	}
	at, id, ok := strings.Cut(string(decoded), "|")
	if !ok {
		return Cursor{}, ErrCursorInvalid
	}
	created, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return Cursor{}, ErrCursorInvalid
	}
	if _, err := uuid.Parse(id); err != nil {
		return Cursor{}, ErrCursorInvalid
	}
	return Cursor{CreatedAt: created, ID: id}, nil
}

type LogQuery struct {
	WorkspaceID string
	Status      MessageStatus
	Recipient   emailaddress.Address
	TagName     string
	TagValue    string
	From        time.Time
	To          time.Time
	Cursor      Cursor
	Limit       int
}

func (q LogQuery) Normalize() (LogQuery, error) {
	if q.Status != "" && !q.Status.Valid() {
		return LogQuery{}, fmt.Errorf("%w: status %q", ErrFilterInvalid, q.Status)
	}
	if strings.TrimSpace(string(q.Recipient)) != "" {
		addr, err := emailaddress.Normalize(string(q.Recipient))
		if err != nil {
			return LogQuery{}, fmt.Errorf("%w: recipient", ErrFilterInvalid)
		}
		q.Recipient = addr
	} else {
		q.Recipient = ""
	}
	if (q.TagName == "") != (q.TagValue == "") {
		return LogQuery{}, fmt.Errorf("%w: a tag filter needs tag_name and tag_value", ErrFilterInvalid)
	}
	if q.TagName != "" && (!ValidTagPart(q.TagName) || !ValidTagPart(q.TagValue)) {
		return LogQuery{}, fmt.Errorf("%w: tag", ErrFilterInvalid)
	}
	if !q.From.IsZero() && !q.To.IsZero() && !q.To.After(q.From) {
		return LogQuery{}, fmt.Errorf("%w: to must come after from", ErrFilterInvalid)
	}
	switch {
	case q.Limit < 0:
		return LogQuery{}, fmt.Errorf("%w: limit", ErrFilterInvalid)
	case q.Limit == 0:
		q.Limit = DefaultPageSize
	case q.Limit > MaxPageSize:
		q.Limit = MaxPageSize
	}
	return q, nil
}

type Keyed interface {
	Keyset() Cursor
}

type Page[T Keyed] struct {
	Items []T
	Next  string
}

func PageOf[T Keyed](rows []T, limit int) Page[T] {
	if len(rows) <= limit {
		return Page[T]{Items: rows}
	}
	items := rows[:limit]
	return Page[T]{Items: items, Next: items[len(items)-1].Keyset().Encode()}
}

func (m Message) Keyset() Cursor { return Cursor{CreatedAt: m.CreatedAt, ID: m.ID} }

type LogEntry struct {
	Message
	Send Send
}

type EventView struct {
	Kind       string
	OccurredAt time.Time
	Link       string
	IsBot      bool
	Detail     []byte
}

type Detail struct {
	Entry  LogEntry
	Events []EventView
}
