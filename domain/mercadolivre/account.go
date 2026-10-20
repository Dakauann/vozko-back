package mercadolivre

import (
	"fmt"
	"strings"
	"time"

	"vozko/domain/conversation"
)

const TokenRefreshLead = 10 * time.Minute

const AccessTokenLifetime = 6 * time.Hour

const RefreshTokenLifetime = 180 * 24 * time.Hour

const PurgeGracePeriod = 30 * 24 * time.Hour

const (
	ScopeOfflineAccess = "offline_access"
	ScopeRead          = "read"
	ScopeWrite         = "write"
)

func RequiredScopes() []string { return []string{ScopeOfflineAccess, ScopeRead, ScopeWrite} }

type Status string

const (
	StatusConnected      Status = "CONNECTED"
	StatusNeedsReconnect Status = "NEEDS_RECONNECT"
	StatusRevoked        Status = "REVOKED"
	StatusDisconnected   Status = "DISCONNECTED"
)

func (s Status) Valid() bool {
	switch s {
	case StatusConnected, StatusNeedsReconnect, StatusRevoked, StatusDisconnected:
		return true
	}
	return false
}

func ParseStatus(raw string) (Status, error) {
	s := Status(raw)
	if !s.Valid() {
		return "", fmt.Errorf("%w: %q", ErrInvalidStatus, raw)
	}
	return s, nil
}

func (s Status) CanTransitionTo(next Status) bool {
	if !s.Valid() || !next.Valid() {
		return false
	}
	if s == next {
		return true
	}
	switch s {
	case StatusConnected:
		return next == StatusNeedsReconnect || next == StatusRevoked || next == StatusDisconnected
	case StatusNeedsReconnect:
		return next == StatusConnected || next == StatusRevoked || next == StatusDisconnected
	case StatusRevoked:
		return next == StatusConnected || next == StatusDisconnected
	case StatusDisconnected:
		return next == StatusConnected
	}
	return false
}

type AccountCapabilities struct {
	CanReadQuestions   bool
	CanAnswerQuestions bool
	CanReadMessages    bool
	CanSendMessages    bool
}

type Credentials struct {
	UserID      string
	AccessToken string
}

func (c Credentials) Validate() error {
	if !ValidNumericID(c.UserID) || strings.TrimSpace(c.AccessToken) == "" {
		return ErrCredentialsInvalid
	}
	return nil
}

type Account struct {
	ID           string  `json:"id"`
	WorkspaceID  string  `json:"workspaceId"`
	DepartmentID *string `json:"departmentId,omitempty"`

	MLUserID string `json:"mlUserId"`
	SiteID   string `json:"siteId"`
	Nickname string `json:"nickname"`

	AccessToken      string     `json:"-"`
	RefreshToken     string     `json:"-"`
	AccessExpiresAt  *time.Time `json:"accessExpiresAt,omitempty"`
	RefreshExpiresAt *time.Time `json:"refreshExpiresAt,omitempty"`
	GrantedScopes    []string   `json:"grantedScopes"`

	CanReadQuestions   bool `json:"canReadQuestions"`
	CanAnswerQuestions bool `json:"canAnswerQuestions"`
	CanReadMessages    bool `json:"canReadMessages"`
	CanSendMessages    bool `json:"canSendMessages"`

	Status       Status `json:"status"`
	StatusReason string `json:"statusReason,omitempty"`

	LastReconciledAt  *time.Time `json:"lastReconciledAt,omitempty"`
	LastHealthCheckAt *time.Time `json:"lastHealthCheckAt,omitempty"`

	AgentID              *string `json:"agentId,omitempty"`
	WorkflowID           *string `json:"workflowId,omitempty"`
	PipelineID           *string `json:"pipelineId,omitempty"`
	EnableAgentResponses bool    `json:"enableAgentResponses"`
	EnableWorkflow       bool    `json:"enableWorkflow"`
	AutomationDisclosure string  `json:"automationDisclosure,omitempty"`

	PurgeAfter *time.Time `json:"purgeAfter,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (a *Account) Validate() error {
	if strings.TrimSpace(a.WorkspaceID) == "" {
		return ErrWorkspaceIDRequired
	}
	if !ValidNumericID(a.MLUserID) {
		return ErrUserIDInvalid
	}
	if !SupportedSite(a.SiteID) {
		return fmt.Errorf("%w: %q", ErrSiteNotSupported, a.SiteID)
	}
	if !a.Status.Valid() {
		return ErrInvalidStatus
	}
	return nil
}

func (a *Account) Capabilities() AccountCapabilities {
	return AccountCapabilities{
		CanReadQuestions:   a.CanReadQuestions,
		CanAnswerQuestions: a.CanAnswerQuestions,
		CanReadMessages:    a.CanReadMessages,
		CanSendMessages:    a.CanSendMessages,
	}
}

func (a *Account) SetCapabilities(c AccountCapabilities) {
	a.CanReadQuestions = c.CanReadQuestions
	a.CanAnswerQuestions = c.CanAnswerQuestions
	a.CanReadMessages = c.CanReadMessages
	a.CanSendMessages = c.CanSendMessages
}

func (a *Account) Credentials() Credentials {
	return Credentials{UserID: a.MLUserID, AccessToken: a.AccessToken}
}

func (a *Account) usable() bool {
	return a.Status == StatusConnected && strings.TrimSpace(a.AccessToken) != ""
}

func (a *Account) CanSend() bool { return a.usable() && a.CanSendMessages }

func (a *Account) CanReceiveMessages() bool { return a.usable() && a.CanReadMessages }

func (a *Account) CanAnswer() bool { return a.usable() && a.CanAnswerQuestions }

func (a *Account) CanReadQuestionsNow() bool { return a.usable() && a.CanReadQuestions }

func (a *Account) TokenNeedsRefresh(now time.Time) bool {
	if a.Status != StatusConnected || strings.TrimSpace(a.RefreshToken) == "" {
		return false
	}
	if a.AccessExpiresAt == nil {
		return true
	}
	return !now.Before(a.AccessExpiresAt.Add(-TokenRefreshLead))
}

func (a *Account) PurgeDue(now time.Time) bool {
	if a.PurgeAfter == nil {
		return false
	}
	if a.Status != StatusRevoked && a.Status != StatusDisconnected {
		return false
	}
	return !now.Before(*a.PurgeAfter)
}

func PurgeScheduleFrom(now time.Time) time.Time { return now.Add(PurgeGracePeriod) }

func (a *Account) DisplayName() string {
	if n := strings.TrimSpace(a.Nickname); n != "" {
		return n
	}
	return a.MLUserID
}

func (a *Account) Automation() conversation.ChannelAutomation {
	return conversation.ChannelAutomation{
		AgentID:              a.AgentID,
		WorkflowID:           a.WorkflowID,
		EnableAgentResponses: a.EnableAgentResponses,
		EnableWorkflow:       a.EnableWorkflow,
		Disclosure:           strings.TrimSpace(a.AutomationDisclosure),
	}
}

func ParseScopes(raw string) []string {
	fields := strings.Fields(raw)
	out := make([]string, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		if _, dup := seen[f]; dup {
			continue
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}
	return out
}

func HasRequiredScopes(granted []string) bool {
	have := make(map[string]struct{}, len(granted))
	for _, s := range granted {
		have[s] = struct{}{}
	}
	for _, s := range RequiredScopes() {
		if _, ok := have[s]; !ok {
			return false
		}
	}
	return true
}
