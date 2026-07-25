package agent

import "time"

type AgentListItem struct {
	ID             string        `json:"id"`
	WorkspaceID    string        `json:"workspaceId"`
	DepartmentID   string        `json:"departmentId,omitempty"`
	Name           string        `json:"name"`
	AvatarURL      string        `json:"avatarUrl"`
	Tags           []string      `json:"tags"`
	Provider       AgentProvider `json:"provider"`
	MessagingModel string        `json:"messagingModel"`
	IsActive       bool          `json:"isActive"`
	Archived       bool          `json:"archived"`
	CreatedAt      time.Time     `json:"createdAt"`
	UpdatedAt      time.Time     `json:"updatedAt"`
}
