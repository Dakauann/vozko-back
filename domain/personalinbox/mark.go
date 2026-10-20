package personalinbox

import (
	"errors"
	"strings"
	"time"

	"vozko/domain/shared"
)

var (
	ErrUserRequired  = errors.New("personalinbox: a mark needs its user")
	ErrEntryRequired = errors.New("personalinbox: a mark needs a conversation")
)

type Mark struct {
	UserID         string
	EntryType      shared.EntryType
	EntryID        string
	PinnedAt       *time.Time
	UnreadMarkedAt *time.Time
}

func NewMark(userID string, entry shared.EntryRef) (Mark, error) {
	userID = strings.TrimSpace(userID)
	entryID := strings.TrimSpace(entry.EntryID)
	if userID == "" {
		return Mark{}, ErrUserRequired
	}
	if entryID == "" || !entry.EntryType.SupportsConversationView() {
		return Mark{}, ErrEntryRequired
	}
	return Mark{UserID: userID, EntryType: entry.EntryType, EntryID: entryID}, nil
}

func (m Mark) Entry() shared.EntryRef {
	return shared.EntryRef{EntryID: m.EntryID, EntryType: m.EntryType}
}

func (m Mark) SetPinned(pinned bool, now time.Time) Mark {
	switch {
	case !pinned:
		m.PinnedAt = nil
	case m.PinnedAt == nil:
		m.PinnedAt = utc(now)
	}
	return m
}

func (m Mark) MarkUnread(now time.Time) Mark {
	if m.UnreadMarkedAt == nil {
		m.UnreadMarkedAt = utc(now)
	}
	return m
}

func (m Mark) MarkRead() Mark {
	m.UnreadMarkedAt = nil
	return m
}

func (m Mark) Pinned() bool { return m.PinnedAt != nil }

func (m Mark) Unread() bool { return m.UnreadMarkedAt != nil }

func (m Mark) Empty() bool {
	return m.PinnedAt == nil && m.UnreadMarkedAt == nil
}

func (m Mark) Same(other Mark) bool {
	return m.UserID == other.UserID &&
		m.Entry() == other.Entry() &&
		sameInstant(m.PinnedAt, other.PinnedAt) &&
		sameInstant(m.UnreadMarkedAt, other.UnreadMarkedAt)
}

func sameInstant(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

func utc(t time.Time) *time.Time {
	u := t.UTC()
	return &u
}
