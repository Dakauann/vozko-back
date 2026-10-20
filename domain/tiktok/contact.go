package tiktok

import (
	"strings"
	"time"
)

const ProfileFreshness = 7 * 24 * time.Hour

type Contact struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	AccountID   string `json:"accountId"`

	UniqueIdentifier   string     `json:"uniqueIdentifier"`
	Username           string     `json:"username,omitempty"`
	DisplayName        string     `json:"displayName,omitempty"`
	AvatarKey          string     `json:"-"`
	IsFollower         *bool      `json:"isFollower,omitempty"`
	ProfileRefreshedAt *time.Time `json:"profileRefreshedAt,omitempty"`

	LeadID *string `json:"leadId,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (c *Contact) Name() string {
	if n := strings.TrimSpace(c.DisplayName); n != "" {
		return n
	}
	if u := strings.TrimSpace(c.Username); u != "" {
		return "@" + u
	}
	return ""
}

func (c *Contact) ProfileIsStale(now time.Time) bool {
	if c.ProfileRefreshedAt == nil {
		return true
	}
	return now.Sub(*c.ProfileRefreshedAt) >= ProfileFreshness
}
