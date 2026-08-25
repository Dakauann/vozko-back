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
	// ErrInvalidIncludedInstances means a negative allowance was submitted.
	// Negative is not "none" — it is a typo, and silently clamping it would hide
	// an admin's mistake behind a number they did not choose.
	ErrInvalidIncludedInstances = errors.New("included unofficial whatsapp instances cannot be negative")
)

// MaxIncludedUnofficialWhatsAppInstances bounds what one workspace may be
// granted directly.
//
// A guard against a fat-fingered admin, not a product limit: every connected
// number occupies a slot on a host with a hard ceiling, so an accidental 1000
// would exhaust a shared host's capacity for every other tenant on it. A
// workspace that genuinely needs more than this is a conversation, not a form.
const MaxIncludedUnofficialWhatsAppInstances = 100

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

type WorkspaceConfig struct {
	ID                         string `json:"id"`
	WorkspaceID                string `json:"workspaceId"`
	CampaignSpamProtectionDays int    `json:"campaignSpamProtectionDays"`
	SkipAdminAssignment        bool   `json:"skipAdminAssignment"`
	// IncludedUnofficialWhatsAppInstances is how many linked-device WhatsApp
	// numbers this workspace may connect before buying more.
	//
	// PLATFORM-ADMIN ONLY, and it lives here rather than on the plan for the
	// reason the entitlement kind documents: the channel has no per-plan pricing,
	// every number occupies a slot on a host we operate, and connecting one that
	// gets banned costs a real customer their WhatsApp. Granting the allowance
	// per workspace keeps that a decision somebody makes rather than a side
	// effect of a plan change.
	//
	// Zero is the default and means "none included": a workspace connects
	// nothing until it is granted an allowance or buys an addon. Defaulting to
	// anything else would hand every existing workspace capacity nobody decided
	// to give them.
	IncludedUnofficialWhatsAppInstances int `json:"includedUnofficialWhatsAppInstances"`

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

type Repository interface {
	GetByWorkspaceID(ctx context.Context, workspaceID string) (*WorkspaceConfig, error)
	Upsert(ctx context.Context, cfg *WorkspaceConfig) error
	EnsureExists(ctx context.Context, workspaceID string) error
	// GetIncludedUnofficialInstancesByWorkspaceIDs reads one config field for
	// many workspaces in a single query.
	//
	// Declared on the interface rather than reached for by type assertion so the
	// compiler forces every implementation to provide it: the entitlement sweep
	// that uses it would otherwise fall back to zero for every tenant and report
	// that nobody is entitled to anything, silently.
	//
	// A workspace with no config row is absent from the result, which the caller
	// reads as zero — the same thing it must already assume for a workspace that
	// was never granted anything.
	GetIncludedUnofficialInstancesByWorkspaceIDs(ctx context.Context, workspaceIDs []string) (map[string]int, error)
}

// UpdateWorkspaceConfigInput is the PLATFORM-ADMIN update.
//
// It is reachable only through the /admin routes, which the router gates on
// RoleAdmin and the usecase re-checks. The workspace-facing update takes
// UpdateWorkspaceConfigOwnerInput and cannot reach any field here — which is the
// whole point for the entitlement below: a workspace administrator must not be
// able to grant themselves more connected numbers.
type UpdateWorkspaceConfigInput struct {
	CampaignSpamProtectionDays *int `json:"campaignSpamProtectionDays,omitempty"`
	// IncludedUnofficialWhatsAppInstances is a pointer so "not sent" and "set it
	// to zero" stay distinguishable — revoking an allowance is a real action and
	// must not be indistinguishable from an unrelated edit.
	IncludedUnofficialWhatsAppInstances *int `json:"includedUnofficialWhatsAppInstances,omitempty"`
}

type UpdateWorkspaceConfigOwnerInput struct {
	SkipAdminAssignment *bool `json:"skipAdminAssignment,omitempty"`
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
