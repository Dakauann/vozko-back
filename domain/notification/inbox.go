package notification

import (
	"strings"
	"time"

	"vozko/domain/shared"
)

const (
	DefaultPageSize  = 20
	MaxPageSize      = 50
	MaxIDsPerChange  = 100
	UnreadCountLimit = 1000
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusDelivered Status = "delivered"
)

type Record struct {
	Draft
	ID             string
	Status         Status
	RecipientCount int
	CreatedAt      time.Time
	DeliveredAt    *time.Time
}

func (r Record) Item() Item {
	return Item{
		ID:          r.ID,
		Category:    r.Category,
		Severity:    r.Severity,
		Title:       r.Title,
		Body:        r.Body,
		ImageURL:    r.ImageURL,
		ActionURL:   r.ActionURL,
		ActionLabel: r.ActionLabel,
		CreatedAt:   r.CreatedAt,
	}
}

func (r Record) EmailDedupKey(userID string) string {
	if r.BroadcastID != "" {
		return "notification:broadcast:" + r.BroadcastID + ":" + userID
	}
	return "notification:" + r.ID + ":" + userID
}

type Recipient struct {
	NotificationID string
	WorkspaceID    string
	UserID         string
	Category       Category
	CreatedAt      time.Time
}

type Item struct {
	ID          string
	Category    Category
	Severity    Severity
	Title       string
	Body        string
	ImageURL    string
	ActionURL   string
	ActionLabel string
	CreatedAt   time.Time
	ReadAt      *time.Time
}

type Viewer struct {
	WorkspaceID string
	UserID      string
}

func (v Viewer) Validate() error {
	if strings.TrimSpace(v.WorkspaceID) == "" {
		return ErrWorkspaceRequired
	}
	if strings.TrimSpace(v.UserID) == "" {
		return ErrViewerRequired
	}
	return nil
}

type ListQuery struct {
	Category   Category
	UnreadOnly bool
	Before     shared.Keyset
	Paged      bool
	Limit      int
}

func ParseListQuery(category string, unreadOnly bool, cursor string, limit int) (ListQuery, error) {
	q := ListQuery{Category: Category(strings.TrimSpace(category)), UnreadOnly: unreadOnly, Limit: limit}
	if q.Category != "" && !q.Category.Valid() {
		return ListQuery{}, ErrCategoryUnknown
	}
	before, paged, err := shared.ParseKeyset(cursor)
	if err != nil {
		return ListQuery{}, ErrCursorInvalid
	}
	q.Before, q.Paged = before, paged
	return q.Normalize(), nil
}

func (q ListQuery) Normalize() ListQuery {
	if q.Limit <= 0 {
		q.Limit = DefaultPageSize
	}
	if q.Limit > MaxPageSize {
		q.Limit = MaxPageSize
	}
	return q
}

type Page struct {
	Items []Item
	Next  string
}

func PageOf(rows []Item, limit int) Page {
	shown, next := pageOf(rows, limit, func(i Item) shared.Keyset { return shared.Keyset{At: i.CreatedAt, ID: i.ID} })
	return Page{Items: shown, Next: next}
}

func pageOf[T any](rows []T, limit int, key func(T) shared.Keyset) ([]T, string) {
	if limit <= 0 || len(rows) <= limit {
		return rows, ""
	}
	shown := rows[:limit]
	return shown, key(shown[len(shown)-1]).Encode()
}

type Summary struct {
	Unread     int
	ByCategory map[Category]int
	Capped     bool
}

func ReadIDs(ids []string) ([]string, error) {
	if len(ids) == 0 || len(ids) > MaxIDsPerChange {
		return nil, ErrIDsInvalid
	}
	out := uniqueTrimmed(ids)
	if len(out) == 0 {
		return nil, ErrIDsInvalid
	}
	return out, nil
}

type Preferences struct {
	WorkspaceID string
	UserID      string
	EmailMuted  []Category
	EmailDigest bool
	UpdatedAt   time.Time
}

func (p Preferences) WantsEmail(category Category, severity Severity) bool {
	if severity == SeverityCritical {
		return true
	}
	for _, muted := range p.EmailMuted {
		if muted == category {
			return false
		}
	}
	return true
}

func MutedCategories(raw []string) ([]Category, error) {
	out := make([]Category, 0, len(raw))
	for _, v := range uniqueTrimmed(raw) {
		c := Category(v)
		if !c.Valid() {
			return nil, ErrCategoryUnknown
		}
		out = append(out, c)
	}
	return out, nil
}
