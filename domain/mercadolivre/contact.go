package mercadolivre

import (
	"strings"
	"time"
)

const ProfileFreshness = 24 * time.Hour

type Contact struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	AccountID   string `json:"accountId"`

	MLUserID           string     `json:"mlUserId"`
	Nickname           string     `json:"nickname,omitempty"`
	ProfileRefreshedAt *time.Time `json:"profileRefreshedAt,omitempty"`

	LeadID *string `json:"leadId,omitempty"`

	QuestionsBlockedAt *time.Time `json:"questionsBlockedAt,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (c *Contact) DisplayName() string {
	if n := strings.TrimSpace(c.Nickname); n != "" {
		return n
	}
	return c.MLUserID
}

func (c *Contact) QuestionsBlocked() bool { return c.QuestionsBlockedAt != nil }

func (c *Contact) ProfileIsStale(now time.Time) bool {
	if c.ProfileRefreshedAt == nil {
		return true
	}
	return now.Sub(*c.ProfileRefreshedAt) >= ProfileFreshness
}
