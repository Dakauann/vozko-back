package workspace_config

import (
	"context"
	"errors"
	"time"
)

var (
	ErrConfigNotFound = errors.New("workspace configuration not found")
	ErrUnauthorized   = errors.New("only admins can modify workspace configuration")
	ErrForbidden      = errors.New("insufficient permissions to modify workspace configuration")
	// ErrInvalidHoldMusicTrack means the selected hold music is neither a known
	// builtin key nor a hold_music media owned by this workspace.
	ErrInvalidHoldMusicTrack = errors.New("invalid hold music track")
	// ErrInvalidQueueOverflow means the queue overflow action is not one of the
	// accepted values ("hangup" | "recall").
	ErrInvalidQueueOverflow = errors.New("invalid queue overflow action")
)

const DefaultCampaignSpamProtectionDays = 3

// Auto-close defaults: both policies ON so the open set does not grow forever.
// Idle: hours after last agent/AI message with no customer reply (waiting-on-customer).
// Max-age: absolute inactivity on last_message_at any side (inbox hygiene floor).
// Industry: Intercom waiting-on-customer + LivePerson long inactivity (~90d ceiling).
const (
	DefaultAutoCloseEnabled        = true
	DefaultAutoCloseIdleAfterHours = 24
	MinAutoCloseIdleAfterHours     = 1
	MaxAutoCloseIdleAfterHours     = 168 // 7 days

	// Max-age default ON: every mature inbox product keeps a long inactivity floor.
	// 7d is the product default for high-volume sales WhatsApp; workspaces can
	// disable or raise up to 90d. Distinct reason max_age (not customer_idle/silence).
	DefaultAutoCloseMaxAgeEnabled    = true
	DefaultAutoCloseMaxAgeAfterHours = 168  // 7 days
	MinAutoCloseMaxAgeAfterHours     = 24   // at least 1 day
	MaxAutoCloseMaxAgeAfterHours     = 2160 // 90 days (LivePerson-scale ceiling)
)

// BuiltinHoldMusicPrefix marks a HoldMusicTrack value that selects one of the
// tracks shipped with the server (e.g. "builtin:lofi") instead of an uploaded
// hold_music media id. Empty selects the system default (env asset or the
// generated comfort tone).
const BuiltinHoldMusicPrefix = "builtin:"

type WorkspaceConfig struct {
	ID                         string `json:"id"`
	WorkspaceID                string `json:"workspaceId"`
	CampaignSpamProtectionDays int    `json:"campaignSpamProtectionDays"`
	SkipAdminAssignment        bool   `json:"skipAdminAssignment"`
	// HoldMusicTrack is what a caller hears while held/parked during transfers:
	// "" (system default), "builtin:<key>" (shipped track) or an uploaded
	// hold_music media id.
	HoldMusicTrack string `json:"holdMusicTrack"`

	// Call-queue (ACD) policy. When QueueEnabled and a blind transfer's target (or a
	// distributed department) has no free agent, the caller is held in a waiting line
	// instead of getting a busy signal, and rung to the next agent who frees up. The
	// bounds guarantee a caller is NEVER waiting forever: QueueMaxWaitSeconds is a hard
	// ceiling and QueueMaxLength caps how many callers wait at once (0 => safe server
	// defaults, hard-capped). QueueOverflow is the terminal at the wait ceiling:
	// "hangup" (announce + hang up) or "recall" (ring the original initiator).
	QueueEnabled        bool   `json:"queueEnabled"`
	QueueMaxWaitSeconds int    `json:"queueMaxWaitSeconds"`
	QueueMaxLength      int    `json:"queueMaxLength"`
	QueueOverflow       string `json:"queueOverflow"`

	// AutoCloseEnabled: when true, a background job finishes conversations after
	// last agent/AI message + customer silence past AutoCloseIdleAfterHours.
	// Default true. Status becomes finished with close_source=system.
	AutoCloseEnabled bool `json:"autoCloseEnabled"`
	// AutoCloseIdleAfterHours: silence window after our last outbound (1..168).
	// 0 or invalid values resolve to DefaultAutoCloseIdleAfterHours on write/read.
	AutoCloseIdleAfterHours int `json:"autoCloseIdleAfterHours"`

	// AutoCloseMaxAgeEnabled: absolute inactivity on last_message_at (any side).
	// Default true. Distinct reason max_age (not customer_idle). Inbox hygiene.
	AutoCloseMaxAgeEnabled bool `json:"autoCloseMaxAgeEnabled"`
	// AutoCloseMaxAgeAfterHours: no message either side for this many hours (24..2160).
	AutoCloseMaxAgeAfterHours int `json:"autoCloseMaxAgeAfterHours"`

	UpdatedBy string    `json:"updatedBy,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ClampAutoCloseIdleHours normalizes idle hours to the allowed range.
func ClampAutoCloseIdleHours(hours int) int {
	if hours < MinAutoCloseIdleAfterHours {
		return DefaultAutoCloseIdleAfterHours
	}
	if hours > MaxAutoCloseIdleAfterHours {
		return MaxAutoCloseIdleAfterHours
	}
	return hours
}

// ClampAutoCloseMaxAgeHours normalizes absolute inactivity hours.
func ClampAutoCloseMaxAgeHours(hours int) int {
	if hours < MinAutoCloseMaxAgeAfterHours {
		return DefaultAutoCloseMaxAgeAfterHours
	}
	if hours > MaxAutoCloseMaxAgeAfterHours {
		return MaxAutoCloseMaxAgeAfterHours
	}
	return hours
}

// EffectiveAutoCloseIdleAfterHours returns the clamped idle window for this config.
func (c *WorkspaceConfig) EffectiveAutoCloseIdleAfterHours() int {
	if c == nil {
		return DefaultAutoCloseIdleAfterHours
	}
	return ClampAutoCloseIdleHours(c.AutoCloseIdleAfterHours)
}

// EffectiveAutoCloseMaxAgeAfterHours returns the clamped max-age window.
func (c *WorkspaceConfig) EffectiveAutoCloseMaxAgeAfterHours() int {
	if c == nil {
		return DefaultAutoCloseMaxAgeAfterHours
	}
	return ClampAutoCloseMaxAgeHours(c.AutoCloseMaxAgeAfterHours)
}

// QueueOverflow* are the valid QueueOverflow values (mirror dialer.QueueOverflow*;
// duplicated as plain strings here to keep this config package free of a dialer
// import).
const (
	QueueOverflowHangup = "hangup"
	QueueOverflowRecall = "recall"
)

func ValidQueueOverflow(v string) bool {
	return v == QueueOverflowHangup || v == QueueOverflowRecall
}

// HoldMusicTrackValidator checks a HoldMusicTrack value against the builtin
// catalog and the workspace's own hold_music media. Implemented in infra (it
// needs the catalog and the media repository); the port keeps this package pure.
type HoldMusicTrackValidator interface {
	ValidateHoldMusicTrack(ctx context.Context, workspaceID, track string) error
}

type Repository interface {
	GetByWorkspaceID(ctx context.Context, workspaceID string) (*WorkspaceConfig, error)
	Upsert(ctx context.Context, cfg *WorkspaceConfig) error
	EnsureExists(ctx context.Context, workspaceID string) error
}

type UpdateWorkspaceConfigInput struct {
	CampaignSpamProtectionDays *int `json:"campaignSpamProtectionDays,omitempty"`
}

type UpdateWorkspaceConfigOwnerInput struct {
	SkipAdminAssignment *bool `json:"skipAdminAssignment,omitempty"`
	// HoldMusicTrack: pointer distinguishes "not sent" from "clear" (empty string
	// resets to the system default).
	HoldMusicTrack *string `json:"holdMusicTrack,omitempty"`

	// Queue policy (all pointers: nil = not sent, so a partial update is safe).
	QueueEnabled        *bool   `json:"queueEnabled,omitempty"`
	QueueMaxWaitSeconds *int    `json:"queueMaxWaitSeconds,omitempty"`
	QueueMaxLength      *int    `json:"queueMaxLength,omitempty"`
	QueueOverflow       *string `json:"queueOverflow,omitempty"`

	// Conversation auto-close policy (nil = not sent).
	AutoCloseEnabled        *bool `json:"autoCloseEnabled,omitempty"`
	AutoCloseIdleAfterHours *int  `json:"autoCloseIdleAfterHours,omitempty"`
	// Absolute inactivity max-age (nil = not sent).
	AutoCloseMaxAgeEnabled    *bool `json:"autoCloseMaxAgeEnabled,omitempty"`
	AutoCloseMaxAgeAfterHours *int  `json:"autoCloseMaxAgeAfterHours,omitempty"`
}

type GetWorkspaceConfigUseCase interface {
	Execute(ctx context.Context, workspaceID string) (*WorkspaceConfig, error)
}

type UpdateWorkspaceConfigUseCase interface {
	Execute(ctx context.Context, workspaceID, userID, userRole string, input UpdateWorkspaceConfigInput) (*WorkspaceConfig, error)
}

type UpdateWorkspaceConfigOwnerUseCase interface {
	Execute(ctx context.Context, workspaceID, callerID, callerRole string, input UpdateWorkspaceConfigOwnerInput) (*WorkspaceConfig, error)
}
