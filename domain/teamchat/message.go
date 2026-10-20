package teamchat

import (
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/shared"
)

type MessageKind string

const (
	MessageText   MessageKind = "text"
	MessageSystem MessageKind = "system"
)

type Event string

const (
	EventCreated       Event = "created"
	EventRenamed       Event = "renamed"
	EventMembersAdded  Event = "members_added"
	EventMemberRemoved Event = "member_removed"
	EventMemberLeft    Event = "member_left"
)

const (
	MaxBodyRunes      = 4000
	MaxMentions       = 50
	MaxClientIDLength = 128
)

type Message struct {
	ID          string
	ThreadID    string
	WorkspaceID string
	Seq         int64
	AuthorID    string
	Kind        MessageKind
	Body        string
	Event       Event
	SubjectIDs  []string
	ReplyToID   string
	Mentions    []string
	ClientID    string
	CreatedAt   time.Time
	EditedAt    *time.Time
	DeletedAt   *time.Time
}

type Draft struct {
	Body      string
	Mentions  []string
	ReplyToID string
	ClientID  string
}

func (d Draft) normalized() (Draft, error) {
	body, err := NormalizeBody(d.Body)
	if err != nil {
		return Draft{}, err
	}
	mentions, err := NormalizeMentions(d.Mentions)
	if err != nil {
		return Draft{}, err
	}
	clientID := strings.TrimSpace(d.ClientID)
	if len(clientID) > MaxClientIDLength {
		return Draft{}, ErrClientIDTooLong
	}
	return Draft{Body: body, Mentions: mentions, ReplyToID: strings.TrimSpace(d.ReplyToID), ClientID: clientID}, nil
}

func NormalizeBody(body string) (string, error) {
	clean := strings.TrimSpace(body)
	if clean == "" {
		return "", ErrBodyRequired
	}
	if utf8.RuneCountInString(clean) > MaxBodyRunes {
		return "", ErrBodyTooLong
	}
	return clean, nil
}

func NormalizeMentions(ids []string) ([]string, error) {
	out := shared.DistinctTrimmed(ids)
	if len(out) > MaxMentions {
		return nil, ErrTooManyMentions
	}
	return out, nil
}

func NewText(id string, t Thread, authorID string, d Draft, now time.Time) (Message, error) {
	clean, err := d.normalized()
	if err != nil {
		return Message{}, err
	}
	return Message{
		ID:          strings.TrimSpace(id),
		ThreadID:    t.ID,
		WorkspaceID: t.WorkspaceID,
		AuthorID:    strings.TrimSpace(authorID),
		Kind:        MessageText,
		Body:        clean.Body,
		ReplyToID:   clean.ReplyToID,
		Mentions:    clean.Mentions,
		ClientID:    clean.ClientID,
		CreatedAt:   now.UTC(),
	}, nil
}

func NewSystem(id string, t Thread, actorID string, event Event, subjects []string, body string, now time.Time) Message {
	return Message{
		ID:          strings.TrimSpace(id),
		ThreadID:    t.ID,
		WorkspaceID: t.WorkspaceID,
		AuthorID:    strings.TrimSpace(actorID),
		Kind:        MessageSystem,
		Body:        strings.TrimSpace(body),
		Event:       event,
		SubjectIDs:  shared.DistinctTrimmed(subjects),
		Mentions:    []string{},
		CreatedAt:   now.UTC(),
	}
}

func (m Message) IsDeleted() bool {
	return m.DeletedAt != nil
}

func (m Message) Mentioned(userID string) bool {
	for _, id := range m.Mentions {
		if id == userID {
			return true
		}
	}
	return false
}

func (m Message) EditBy(userID, body string, now time.Time) (Message, error) {
	if err := m.changeableBy(userID); err != nil {
		return Message{}, err
	}
	clean, err := NormalizeBody(body)
	if err != nil {
		return Message{}, err
	}
	at := now.UTC()
	m.Body = clean
	m.EditedAt = &at
	return m, nil
}

func (m Message) DeleteBy(userID string, now time.Time) (Message, error) {
	if err := m.changeableBy(userID); err != nil {
		return Message{}, err
	}
	at := now.UTC()
	m.Body = ""
	m.Mentions = []string{}
	m.DeletedAt = &at
	return m, nil
}

func (m Message) changeableBy(userID string) error {
	switch {
	case m.IsDeleted():
		return ErrMessageNotFound
	case m.Kind == MessageSystem:
		return ErrSystemMessage
	case strings.TrimSpace(userID) == "" || userID != m.AuthorID:
		return ErrNotAuthor
	}
	return nil
}
