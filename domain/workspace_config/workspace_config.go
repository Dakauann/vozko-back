package workspace_config

import (
	"context"
	"errors"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/working_hours"
)

var (
	ErrConfigNotFound           = errors.New("workspace configuration not found")
	ErrUnauthorized             = errors.New("only admins can modify workspace configuration")
	ErrForbidden                = errors.New("insufficient permissions to modify workspace configuration")
	ErrInvalidIncludedInstances = errors.New("included unofficial whatsapp instances cannot be negative")
)

const MaxIncludedUnofficialWhatsAppInstances = 100

const DefaultCampaignSpamProtectionDays = 3

const (
	DefaultAutoCloseEnabled        = true
	DefaultAutoCloseIdleAfterHours = 24
	MinAutoCloseIdleAfterHours     = 1
	MaxAutoCloseIdleAfterHours     = 168

	DefaultAutoCloseMaxAgeEnabled    = true
	DefaultAutoCloseMaxAgeAfterHours = 168
	MinAutoCloseMaxAgeAfterHours     = 24
	MaxAutoCloseMaxAgeAfterHours     = 2160
)

const (
	RouletteModeOnline   = "online"
	RouletteModeLastSeen = "last_seen"
)

const (
	DefaultRouletteMode = RouletteModeOnline

	DefaultRouletteLastSeenWindowHours = 48
	MinRouletteLastSeenWindowHours     = 1
	MaxRouletteLastSeenWindowHours     = 168

	DefaultRouletteRescueEnabled      = true
	DefaultRouletteRescueAfterMinutes = 15
	MinRouletteRescueAfterMinutes     = 1
	MaxRouletteRescueAfterMinutes     = 1440
)

type WorkspaceConfig struct {
	ID                                  string `json:"id"`
	WorkspaceID                         string `json:"workspaceId"`
	CampaignSpamProtectionDays          int    `json:"campaignSpamProtectionDays"`
	SkipAdminAssignment                 bool   `json:"skipAdminAssignment"`
	IncludedUnofficialWhatsAppInstances int    `json:"includedUnofficialWhatsAppInstances"`

	AutoCloseEnabled        bool `json:"autoCloseEnabled"`
	AutoCloseIdleAfterHours int  `json:"autoCloseIdleAfterHours"`

	AutoCloseMaxAgeEnabled    bool `json:"autoCloseMaxAgeEnabled"`
	AutoCloseMaxAgeAfterHours int  `json:"autoCloseMaxAgeAfterHours"`

	RouletteMode                string `json:"rouletteMode"`
	RouletteLastSeenWindowHours int    `json:"rouletteLastSeenWindowHours"`
	RouletteRescueEnabled       bool   `json:"rouletteRescueEnabled"`
	RouletteRescueAfterMinutes  int    `json:"rouletteRescueAfterMinutes"`

	WorkingHours *working_hours.Spec `json:"workingHours,omitempty"`

	OutcomeCapture *conversation.OutcomeCapture `json:"outcomeCapture,omitempty"`

	AudienceDailyCap        int `json:"audienceDailyCap"`
	AudienceDebounceMinutes int `json:"audienceDebounceMinutes"`

	UpdatedBy string    `json:"updatedBy,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func ClampAutoCloseIdleHours(hours int) int {
	if hours < MinAutoCloseIdleAfterHours {
		return DefaultAutoCloseIdleAfterHours
	}
	if hours > MaxAutoCloseIdleAfterHours {
		return MaxAutoCloseIdleAfterHours
	}
	return hours
}

func ClampAutoCloseMaxAgeHours(hours int) int {
	if hours < MinAutoCloseMaxAgeAfterHours {
		return DefaultAutoCloseMaxAgeAfterHours
	}
	if hours > MaxAutoCloseMaxAgeAfterHours {
		return MaxAutoCloseMaxAgeAfterHours
	}
	return hours
}

func (c *WorkspaceConfig) EffectiveAutoCloseIdleAfterHours() int {
	if c == nil {
		return DefaultAutoCloseIdleAfterHours
	}
	return ClampAutoCloseIdleHours(c.AutoCloseIdleAfterHours)
}

func (c *WorkspaceConfig) EffectiveAutoCloseMaxAgeAfterHours() int {
	if c == nil {
		return DefaultAutoCloseMaxAgeAfterHours
	}
	return ClampAutoCloseMaxAgeHours(c.AutoCloseMaxAgeAfterHours)
}

func NormalizeRouletteMode(mode string) string {
	switch mode {
	case RouletteModeOnline, RouletteModeLastSeen:
		return mode
	default:
		return DefaultRouletteMode
	}
}

func ClampRouletteLastSeenWindowHours(hours int) int {
	if hours < MinRouletteLastSeenWindowHours {
		return DefaultRouletteLastSeenWindowHours
	}
	if hours > MaxRouletteLastSeenWindowHours {
		return MaxRouletteLastSeenWindowHours
	}
	return hours
}

func ClampRouletteRescueMinutes(minutes int) int {
	if minutes < MinRouletteRescueAfterMinutes {
		return DefaultRouletteRescueAfterMinutes
	}
	if minutes > MaxRouletteRescueAfterMinutes {
		return MaxRouletteRescueAfterMinutes
	}
	return minutes
}

func (c *WorkspaceConfig) EffectiveRouletteMode() string {
	if c == nil {
		return DefaultRouletteMode
	}
	return NormalizeRouletteMode(c.RouletteMode)
}

func (c *WorkspaceConfig) EffectiveRouletteLastSeenWindow() time.Duration {
	if c == nil {
		return time.Duration(DefaultRouletteLastSeenWindowHours) * time.Hour
	}
	return time.Duration(ClampRouletteLastSeenWindowHours(c.RouletteLastSeenWindowHours)) * time.Hour
}

func (c *WorkspaceConfig) EffectiveRouletteRescueAfter() time.Duration {
	if c == nil {
		return time.Duration(DefaultRouletteRescueAfterMinutes) * time.Minute
	}
	return time.Duration(ClampRouletteRescueMinutes(c.RouletteRescueAfterMinutes)) * time.Minute
}

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
	GetIncludedUnofficialInstancesByWorkspaceIDs(ctx context.Context, workspaceIDs []string) (map[string]int, error)
	ListRoulettePolicies(ctx context.Context) ([]RoulettePolicy, error)
}

type RoulettePolicy struct {
	WorkspaceID    string
	RescueAfter    time.Duration
	LastSeenWindow time.Duration
	WorkingHours   *working_hours.Spec
}

type UpdateWorkspaceConfigInput struct {
	CampaignSpamProtectionDays          *int `json:"campaignSpamProtectionDays,omitempty"`
	IncludedUnofficialWhatsAppInstances *int `json:"includedUnofficialWhatsAppInstances,omitempty"`
}

type UpdateWorkspaceConfigOwnerInput struct {
	SkipAdminAssignment         *bool   `json:"skipAdminAssignment,omitempty"`
	AutoCloseEnabled            *bool   `json:"autoCloseEnabled,omitempty"`
	AutoCloseIdleAfterHours     *int    `json:"autoCloseIdleAfterHours,omitempty"`
	AutoCloseMaxAgeEnabled      *bool   `json:"autoCloseMaxAgeEnabled,omitempty"`
	AutoCloseMaxAgeAfterHours   *int    `json:"autoCloseMaxAgeAfterHours,omitempty"`
	RouletteMode                *string `json:"rouletteMode,omitempty"`
	RouletteLastSeenWindowHours *int    `json:"rouletteLastSeenWindowHours,omitempty"`
	RouletteRescueEnabled       *bool   `json:"rouletteRescueEnabled,omitempty"`
	RouletteRescueAfterMinutes  *int    `json:"rouletteRescueAfterMinutes,omitempty"`

	WorkingHours      *working_hours.Spec `json:"workingHours,omitempty"`
	ClearWorkingHours bool                `json:"-"`

	OutcomeCapture      *conversation.OutcomeCapture `json:"outcomeCapture,omitempty"`
	ClearOutcomeCapture bool                         `json:"-"`
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
