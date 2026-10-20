package tiktok

import (
	"context"
	"fmt"
	"strings"
	"time"

	"vozko/domain/conversation"
)

const TokenRefreshLead = 6 * time.Hour

const ReconnectWarningLead = 30 * 24 * time.Hour

const PurgeGracePeriod = 30 * 24 * time.Hour

const (
	ScopeUserInfoBasic    = "user.info.basic"
	ScopeUserInfoUsername = "user.info.username"
	ScopeUserInfoProfile  = "user.info.profile"
	ScopeAccountType      = "user.account.type"
	ScopeVideoList        = "video.list"
	ScopeCommentList      = "comment.list"
	ScopeCommentManage    = "comment.list.manage"
	ScopeMessageRead      = "message.list.read"
	ScopeMessageSend      = "message.list.send"
	ScopeMessageManage    = "message.list.manage"
)

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
	CanReadVideos         bool `json:"canReadVideos"`
	CanReadComments       bool `json:"canReadComments"`
	CanManageComments     bool `json:"canManageComments"`
	CanReceiveMessages    bool `json:"canReceiveMessages"`
	CanSendMessages       bool `json:"canSendMessages"`
	CanManageInboxProfile bool `json:"canManageInboxProfile"`
}

func (c AccountCapabilities) Useful() bool { return c.CanReadComments || c.CanReceiveMessages }

func MessagingEligible(hasAdvancedAccess, isBusinessAccount bool) bool {
	return hasAdvancedAccess || isBusinessAccount
}

func CapabilitiesFrom(scopes []string, messagingEligible bool) AccountCapabilities {
	have := make(map[string]struct{}, len(scopes))
	for _, s := range scopes {
		have[s] = struct{}{}
	}
	all := func(required ...string) bool {
		for _, s := range required {
			if _, ok := have[s]; !ok {
				return false
			}
		}
		return true
	}
	caps := AccountCapabilities{
		CanReadVideos:     all(ScopeVideoList),
		CanReadComments:   all(ScopeVideoList, ScopeCommentList),
		CanManageComments: all(ScopeVideoList, ScopeCommentList, ScopeCommentManage),
	}
	if messagingEligible {
		caps.CanReceiveMessages = all(ScopeMessageRead)
		caps.CanSendMessages = all(ScopeMessageRead, ScopeMessageSend)
		caps.CanManageInboxProfile = all(ScopeMessageRead, ScopeMessageManage)
	}
	return caps
}

func ParseScopes(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' })
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

type Credentials struct {
	OpenID      string
	AccessToken string
}

type Account struct {
	ID           string  `json:"id"`
	WorkspaceID  string  `json:"workspaceId"`
	DepartmentID *string `json:"departmentId,omitempty"`

	OpenID            string `json:"openId"`
	UniqueIdentifier  string `json:"-"`
	Username          string `json:"username"`
	DisplayName       string `json:"displayName"`
	AvatarKey         string `json:"-"`
	HasAdvancedAccess bool   `json:"hasAdvancedAccess"`
	IsBusinessAccount bool   `json:"isBusinessAccount"`

	AccessToken      string     `json:"-"`
	RefreshToken     string     `json:"-"`
	AccessExpiresAt  *time.Time `json:"accessExpiresAt,omitempty"`
	RefreshExpiresAt *time.Time `json:"refreshExpiresAt,omitempty"`
	GrantedScopes    []string   `json:"grantedScopes"`

	Capabilities AccountCapabilities `json:"capabilities"`

	Status       Status `json:"status"`
	StatusReason string `json:"statusReason,omitempty"`

	WebhooksHealthy   bool       `json:"webhooksHealthy"`
	LastHealthCheckAt *time.Time `json:"lastHealthCheckAt,omitempty"`

	AgentID              *string `json:"agentId,omitempty"`
	WorkflowID           *string `json:"workflowId,omitempty"`
	PipelineID           *string `json:"pipelineId,omitempty"`
	EnableAgentResponses bool    `json:"enableAgentResponses"`
	EnableWorkflow       bool    `json:"enableWorkflow"`
	EnableAnalysis       bool    `json:"enableAnalysis"`
	EnableAutoStaging    bool    `json:"enableAutoStaging"`
	EnableAutoMemory     bool    `json:"enableAutoMemory"`
	AutomationDisclosure string  `json:"automationDisclosure,omitempty"`

	PurgeAfter *time.Time `json:"purgeAfter,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (a *Account) Validate() error {
	if strings.TrimSpace(a.WorkspaceID) == "" {
		return ErrWorkspaceIDRequired
	}
	if !ValidOpaqueID(a.OpenID) {
		return ErrOpenIDInvalid
	}
	if !a.Status.Valid() {
		return ErrInvalidStatus
	}
	return nil
}

func (a *Account) Credentials() Credentials {
	return Credentials{OpenID: a.OpenID, AccessToken: a.AccessToken}
}

func (a *Account) usable() bool {
	return a.Status == StatusConnected && strings.TrimSpace(a.AccessToken) != ""
}

func (a *Account) CanSend() bool { return a.usable() && a.Capabilities.CanSendMessages }

func (a *Account) CanReceiveMessagesNow() bool {
	return a.usable() && a.Capabilities.CanReceiveMessages
}

func (a *Account) CanReadCommentsNow() bool { return a.usable() && a.Capabilities.CanReadComments }

func (a *Account) CanManageCommentsNow() bool { return a.usable() && a.Capabilities.CanManageComments }

func (a *Account) CanReadVideosNow() bool { return a.usable() && a.Capabilities.CanReadVideos }

func (a *Account) CanManageInboxProfileNow() bool {
	return a.usable() && a.Capabilities.CanManageInboxProfile
}

func (a *Account) TokenNeedsRefresh(now time.Time) bool {
	if a.Status != StatusConnected || strings.TrimSpace(a.RefreshToken) == "" {
		return false
	}
	if a.AccessExpiresAt == nil {
		return true
	}
	return !now.Before(a.AccessExpiresAt.Add(-TokenRefreshLead))
}

func (a *Account) ReconnectSoon(now time.Time) bool {
	if a.RefreshExpiresAt == nil {
		return false
	}
	return !now.Before(a.RefreshExpiresAt.Add(-ReconnectWarningLead))
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

func (a *Account) Name() string {
	if n := strings.TrimSpace(a.DisplayName); n != "" {
		return n
	}
	if u := strings.TrimSpace(a.Username); u != "" {
		return "@" + u
	}
	return a.OpenID
}

func (a *Account) Automation() conversation.ChannelAutomation {
	return conversation.ChannelAutomation{
		AgentID:              a.AgentID,
		WorkflowID:           a.WorkflowID,
		EnableAgentResponses: a.EnableAgentResponses,
		EnableWorkflow:       a.EnableWorkflow,
		EnableAnalysis:       a.EnableAnalysis,
		EnableAutoStaging:    a.EnableAutoStaging,
		EnableAutoMemory:     a.EnableAutoMemory,
		Disclosure:           strings.TrimSpace(a.AutomationDisclosure),
	}
}

type backgroundKey struct{}

func WithBackground(ctx context.Context) context.Context {
	return context.WithValue(ctx, backgroundKey{}, true)
}

func IsBackground(ctx context.Context) bool {
	background, _ := ctx.Value(backgroundKey{}).(bool)
	return background
}
