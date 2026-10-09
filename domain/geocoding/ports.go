package geocoding

import (
	"context"
	"time"

	"vozko/domain/lead"
)

const (
	Lease         = 5 * time.Minute
	BatchSize     = 500
	WorkspaceTurn = 100
	SweepBudget   = 50 * time.Second
	AnnounceLimit = 50
)

type Claim struct {
	AddressID   string
	WorkspaceID string
	LeadID      string
	Address     lead.Address
	Fingerprint string
	Attempts    int
}

type ClaimRequest struct {
	Token         string
	Now           time.Time
	Lease         time.Duration
	Limit         int
	WorkspaceTurn int
	After         string
}

type Settlement struct {
	Claim      Claim
	Resolution Resolution
}

type SettleResult struct {
	Written    int
	Stale      int
	Leads      int
	Changed    []lead.Change
	Workspaces []string
}

type Queue interface {
	Arm(ctx context.Context, now time.Time) (int64, error)
	Claim(ctx context.Context, req ClaimRequest) ([]Claim, error)
	Settle(ctx context.Context, token string, now time.Time, settlements []Settlement) (SettleResult, error)
	Release(ctx context.Context, token string, addressIDs []string) error
	Backlog(ctx context.Context) (map[lead.GeoStatus]int64, error)
	RefreshAggregates(workspaceIDs []string)
	WakeUnavailable(ctx context.Context, now time.Time) (int64, error)
}

type SettingsStore interface {
	Settings(ctx context.Context, workspaceID string) (Settings, error)
	SettingsOf(ctx context.Context, workspaceIDs []string) (map[string]Settings, error)
	ChangeSettings(ctx context.Context, workspaceID string, change func(Settings) (Settings, error)) (Settings, error)
}

type UsageStore interface {
	TakeSlot(ctx context.Context, workspaceID string, slot Slot) (bool, Usage, error)
	Usage(ctx context.Context, workspaceID string) (Usage, error)
}
