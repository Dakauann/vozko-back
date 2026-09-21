package lead_memory

import (
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/actor"
)

const (
	MaxContentLen = 600

	MaxActiveMemoriesPerLead = 100

	MaxPromptMemories = 40
	MaxPromptChars    = 6000

	MinIDPrefixLen = 8
)

type Category string

const (
	CategoryPersonal   Category = "personal"
	CategoryPreference Category = "preference"
	CategoryDeal       Category = "deal"
	CategoryObjection  Category = "objection"
	CategoryCommitment Category = "commitment"
	CategoryEvent      Category = "event"
	CategoryOther      Category = "other"
)

func AllCategories() []Category {
	return []Category{
		CategoryPersonal,
		CategoryPreference,
		CategoryDeal,
		CategoryObjection,
		CategoryCommitment,
		CategoryEvent,
		CategoryOther,
	}
}

func (c Category) Valid() bool {
	switch c {
	case CategoryPersonal, CategoryPreference, CategoryDeal,
		CategoryObjection, CategoryCommitment, CategoryEvent, CategoryOther:
		return true
	default:
		return false
	}
}

type LeadMemory struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	LeadID      string `json:"leadId"`

	Category Category `json:"category"`
	Content  string   `json:"content"`

	ActorKind actor.Kind `json:"actorKind"`
	ActorID   string     `json:"actorId"`

	SourceEntryID   *string `json:"sourceEntryId,omitempty"`
	SourceEntryType *string `json:"sourceEntryType,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (m *LeadMemory) Normalize() {
	m.Content = strings.TrimSpace(m.Content)
	if m.Category == "" {
		m.Category = CategoryOther
	}
	m.ActorKind, m.ActorID = actor.Normalize(m.ActorKind, m.ActorID)
	m.SourceEntryID = trimmedOrNil(m.SourceEntryID)
	m.SourceEntryType = trimmedOrNil(m.SourceEntryType)
}

func (m *LeadMemory) Validate() error {
	if strings.TrimSpace(m.WorkspaceID) == "" {
		return ErrWorkspaceRequired
	}
	if strings.TrimSpace(m.LeadID) == "" {
		return ErrLeadRequired
	}
	if m.Content == "" {
		return ErrContentRequired
	}
	if utf8.RuneCountInString(m.Content) > MaxContentLen {
		return ErrContentTooLong
	}
	if !m.Category.Valid() {
		return ErrInvalidCategory
	}
	if !m.ActorKind.Valid() || strings.TrimSpace(m.ActorID) == "" {
		return ErrActorRequired
	}
	return nil
}

func NormalizeContent(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

func trimmedOrNil(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
