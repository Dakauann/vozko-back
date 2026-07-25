package callpermission

import (
	"strings"
	"time"
)

type Status string

const (
	StatusPending  Status = "pending"
	StatusGranted  Status = "granted"
	StatusRejected Status = "rejected"
	StatusExpired  Status = "expired"
)

type CallPermission struct {
	ID              string     `json:"id"`
	WorkspaceID     string     `json:"workspaceId"`
	BusinessPhoneID string     `json:"businessPhoneId"`
	LeadID          string     `json:"leadId,omitempty"`
	UserNumber      string     `json:"userNumber"`
	Status          Status     `json:"status"`
	ExpiresAt       *time.Time `json:"expiresAt,omitempty"`
	ResponseSource  string     `json:"responseSource,omitempty"`
	RequestedAt     *time.Time `json:"requestedAt,omitempty"`
	RespondedAt     *time.Time `json:"respondedAt,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

func (p *CallPermission) IsActive(now time.Time) bool {
	if p == nil || p.Status != StatusGranted {
		return false
	}
	if p.ExpiresAt == nil {
		return true
	}
	return now.Before(*p.ExpiresAt)
}

func NormalizeNumber(n string) string {
	return strings.TrimPrefix(strings.TrimSpace(n), "+")
}
