package workspace_config

import (
	"context"
	"errors"
	"time"

	"vozko/domain/working_hours"
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

// Roulette modes: how the inbound roulette picks an owner.
//
//   - online: the pool is whoever holds an open conversation socket right now.
//     The historical behaviour, and the default, so an upgrade changes nothing
//     for any existing workspace.
//   - last_seen: connection state stops being a filter. The pool is every
//     roulette-eligible member who was online at least once inside the
//     configured window, ordered most-recently-online first. A member past the
//     window does not enter the ring at all.
const (
	RouletteModeOnline   = "online"
	RouletteModeLastSeen = "last_seen"
)

const (
	DefaultRouletteMode = RouletteModeOnline

	// 48h is the product default: two days is the longest absence after which
	// "was online once" still predicts "will read this soon". The ceiling is 7
	// days rather than unbounded, because past a week the mode quietly degrades
	// into "assign to anyone who ever logged in", which is not distribution.
	DefaultRouletteLastSeenWindowHours = 48
	MinRouletteLastSeenWindowHours     = 1
	MaxRouletteLastSeenWindowHours     = 168

	// Rescue reassigns a conversation whose owner never opened it. Default on,
	// but only ACTIVE in last_seen mode (see RouletteRescueActive): in online
	// mode the owner was connected when they got it, and rescuing there would
	// be a behaviour change nobody asked for.
	DefaultRouletteRescueEnabled      = true
	DefaultRouletteRescueAfterMinutes = 15
	MinRouletteRescueAfterMinutes     = 1
	MaxRouletteRescueAfterMinutes     = 1440 // 24h
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

	// RouletteMode selects how the inbound roulette builds its pool:
	// RouletteModeOnline (default) or RouletteModeLastSeen.
	RouletteMode string `json:"rouletteMode"`
	// RouletteLastSeenWindowHours: how long after going offline a member stays
	// in the ring (1..168). Only read in last_seen mode.
	RouletteLastSeenWindowHours int `json:"rouletteLastSeenWindowHours"`
	// RouletteRescueEnabled: reassign a conversation whose owner never opened
	// it. Only acts in last_seen mode; see RouletteRescueActive.
	RouletteRescueEnabled bool `json:"rouletteRescueEnabled"`
	// RouletteRescueAfterMinutes: how long the owner has to open or answer
	// before the conversation moves on (1..1440).
	RouletteRescueAfterMinutes int `json:"rouletteRescueAfterMinutes"`

	// WorkingHours is the workspace's weekly schedule. Nil means none is
	// configured, which is always open — the historical behaviour, and why this
	// needs no migration of existing rows.
	//
	// It gates the rescue sweep two ways: a closed scope is not swept at all,
	// and the rescue deadline accrues only while the scope is open, so an agent
	// handed a conversation at 17:55 still gets their full fifteen working
	// minutes rather than losing ten of them overnight.
	WorkingHours *working_hours.Spec `json:"workingHours,omitempty"`

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

// NormalizeRouletteMode resolves anything that is not a known mode to the
// default.
//
// Unknown is normalized rather than rejected, matching how the auto-close hours
// treat an out-of-range value: an admin cannot submit one through the API, so a
// bad value can only arrive from hand-written SQL or an older writer, and
// falling back to the historical behaviour is the safe reading of "we do not
// know what this workspace wants".
func NormalizeRouletteMode(mode string) string {
	switch mode {
	case RouletteModeOnline, RouletteModeLastSeen:
		return mode
	default:
		return DefaultRouletteMode
	}
}

// ClampRouletteLastSeenWindowHours normalizes the last-seen window.
func ClampRouletteLastSeenWindowHours(hours int) int {
	if hours < MinRouletteLastSeenWindowHours {
		return DefaultRouletteLastSeenWindowHours
	}
	if hours > MaxRouletteLastSeenWindowHours {
		return MaxRouletteLastSeenWindowHours
	}
	return hours
}

// ClampRouletteRescueMinutes normalizes the rescue deadline.
func ClampRouletteRescueMinutes(minutes int) int {
	if minutes < MinRouletteRescueAfterMinutes {
		return DefaultRouletteRescueAfterMinutes
	}
	if minutes > MaxRouletteRescueAfterMinutes {
		return MaxRouletteRescueAfterMinutes
	}
	return minutes
}

// EffectiveRouletteMode returns the mode this workspace actually runs.
func (c *WorkspaceConfig) EffectiveRouletteMode() string {
	if c == nil {
		return DefaultRouletteMode
	}
	return NormalizeRouletteMode(c.RouletteMode)
}

// EffectiveRouletteLastSeenWindow returns the clamped last-seen window.
func (c *WorkspaceConfig) EffectiveRouletteLastSeenWindow() time.Duration {
	if c == nil {
		return time.Duration(DefaultRouletteLastSeenWindowHours) * time.Hour
	}
	return time.Duration(ClampRouletteLastSeenWindowHours(c.RouletteLastSeenWindowHours)) * time.Hour
}

// EffectiveRouletteRescueAfter returns the clamped rescue deadline.
func (c *WorkspaceConfig) EffectiveRouletteRescueAfter() time.Duration {
	if c == nil {
		return time.Duration(DefaultRouletteRescueAfterMinutes) * time.Minute
	}
	return time.Duration(ClampRouletteRescueMinutes(c.RouletteRescueAfterMinutes)) * time.Minute
}

// RouletteRescueActive is the single answer to "should the rescue sweep touch
// this workspace".
//
// Rescue only means anything for a pool that can contain somebody who is not
// looking at the screen, so it is gated on the mode as well as on its own
// toggle. That gate is what keeps the default-mode workspaces — every existing
// one — completely untouched by this feature.
func (c *WorkspaceConfig) RouletteRescueActive() bool {
	if c == nil {
		return false
	}
	return c.EffectiveRouletteMode() == RouletteModeLastSeen && c.RouletteRescueEnabled
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
	// ListRoulettePolicies returns ONLY the workspaces whose roulette runs in
	// last_seen mode with rescue enabled.
	//
	// The rescue sweep drives off this, which is what makes it free for
	// everyone else: a workspace on the default mode is filtered out by one
	// indexed read, before any assignment table is touched. It is also why
	// switching the mode back to online stops pending rescues on the next tick
	// with no cleanup pass and no flag to unset on any row.
	ListRoulettePolicies(ctx context.Context) ([]RoulettePolicy, error)
}

// RoulettePolicy is one workspace's resolved (already clamped) rescue settings.
type RoulettePolicy struct {
	WorkspaceID    string
	RescueAfter    time.Duration
	LastSeenWindow time.Duration
	// WorkingHours rides along on the policy list rather than being fetched per
	// workspace later. The sweep needs it BEFORE its candidate query, to drop
	// workspaces where nothing can be due, and the policy query already reads
	// exactly the rows it lives on — so carrying it here costs no extra query
	// while a separate read would cost one per eligible workspace per minute.
	//
	// Nil means no schedule is configured, which is always open.
	WorkingHours *working_hours.Spec
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
	// Roulette distribution policy (nil = not sent).
	RouletteMode                *string `json:"rouletteMode,omitempty"`
	RouletteLastSeenWindowHours *int    `json:"rouletteLastSeenWindowHours,omitempty"`
	RouletteRescueEnabled       *bool   `json:"rouletteRescueEnabled,omitempty"`
	RouletteRescueAfterMinutes  *int    `json:"rouletteRescueAfterMinutes,omitempty"`

	// WorkingHours sets the workspace schedule. Nil means "not sent", exactly
	// like every field above.
	//
	// ClearWorkingHours is separate because a pointer cannot tell "absent" from
	// "explicitly null", and those mean opposite things here: leave the schedule
	// alone, versus go back to operating around the clock. The HTTP layer, which
	// is the only place that can see the difference, translates a JSON null into
	// this flag.
	WorkingHours      *working_hours.Spec `json:"workingHours,omitempty"`
	ClearWorkingHours bool                `json:"-"`
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
