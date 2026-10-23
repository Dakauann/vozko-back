package emailcampaign

import (
	"context"
	"time"
)

type ListQuery struct {
	Status Status
	Search string
	Offset int
	Limit  int
}

type Launch struct {
	StartedAt  time.Time
	LaunchedBy string
}

type Repository interface {
	Create(ctx context.Context, c *Campaign) error
	Get(ctx context.Context, workspaceID, id string) (*Campaign, error)
	GetByID(ctx context.Context, id string) (*Campaign, error)
	Update(ctx context.Context, c *Campaign, from Status) error
	Delete(ctx context.Context, workspaceID, id string) error
	List(ctx context.Context, workspaceID string, q ListQuery) ([]Campaign, int64, error)
	SwapStatus(ctx context.Context, id string, from, to Status, reason HoldReason) (bool, error)
	Schedule(ctx context.Context, id string, from Status, at time.Time, launchedBy string) (bool, error)
	MarkLaunched(ctx context.Context, id string, from Status, l Launch) (bool, error)
	SetSettlement(ctx context.Context, id string, s RunSettlement) (bool, error)
	SetWinner(ctx context.Context, id string, winner Variant) (bool, error)
	SetTestBatch(ctx context.Context, id string, from TestBatchState, tb TestBatch) (bool, error)
	ListTesting(ctx context.Context, limit int) ([]Campaign, error)
	Complete(ctx context.Context, id string, at time.Time) (bool, error)
	ListByStatus(ctx context.Context, statuses ...Status) ([]Campaign, error)
	ListScheduledDue(ctx context.Context, now time.Time, limit int) ([]Campaign, error)
	ListAwaitingWinner(ctx context.Context, now time.Time, limit int) ([]Campaign, error)
	ClaimScheduledStart(campaignID string, dueBy time.Time) (bool, error)
	Names(ctx context.Context, workspaceID string, ids []string) (map[string]string, error)
}

type BillingStore interface {
	BillingState(ctx context.Context, campaignID string) (BillingState, error)
	OpenBatch(ctx context.Context, campaignID string, b ChargeBatch) (ChargeBatch, error)
	MarkBatchCharged(ctx context.Context, campaignID string, batchNo int, at time.Time) (bool, error)
	MarkBatchRefunded(ctx context.Context, campaignID string, batchNo int, units int64, at time.Time) (bool, error)
	ClaimSlot(ctx context.Context, campaignID, recipientID string, at time.Time) (bool, error)
	ReleaseSlot(ctx context.Context, campaignID, recipientID string) (bool, error)
	FreshClaims(ctx context.Context, campaignID string, since time.Time) (int64, error)
	ListUnsettled(ctx context.Context, limit int) ([]Campaign, error)
}

type RecipientRepository interface {
	Replace(ctx context.Context, workspaceID, campaignID string, recipients []Recipient) error
	Get(ctx context.Context, id string) (*Recipient, error)
	MarkSent(ctx context.Context, id, messageID string, at time.Time) (bool, error)
	MarkSkipped(ctx context.Context, id string, reason SkipReason) (bool, error)
	MarkFailed(ctx context.Context, id, code string) (bool, error)
	Counts(ctx context.Context, campaignID string) (RecipientCounts, error)
	PendingIDs(ctx context.Context, campaignID string, variants []Variant, after string, limit int) ([]string, error)
	PendingThrough(ctx context.Context, campaignID, lastID string) (int64, error)
	List(ctx context.Context, workspaceID, campaignID string, f RecipientFilter, offset, limit int) ([]RecipientRow, int64, error)
}

type CampaignSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Sent      int64  `json:"sent"`
	Delivered int64  `json:"delivered"`
	Opens     int64  `json:"opens"`
	Clicks    int64  `json:"clicks"`
	OpenRate  int    `json:"open_rate"`
	ClickRate int    `json:"click_rate"`
}

type OverviewData struct {
	Totals Tally
	Daily  []TimelinePoint
	Top    []CampaignSummary
}

type MetricsReader interface {
	Tally(ctx context.Context, workspaceID, campaignID string) (Tally, error)
	Summaries(ctx context.Context, workspaceID string, campaignIDs []string) (map[string]Tally, error)
	VariantResults(ctx context.Context, workspaceID, campaignID string) (map[Variant]VariantResult, error)
	Timeline(ctx context.Context, workspaceID, campaignID string, bucket Bucket) ([]TimelinePoint, error)
	Links(ctx context.Context, workspaceID, campaignID string, limit int) ([]LinkStat, error)
	Domains(ctx context.Context, workspaceID, campaignID string) ([]DomainCount, error)
	Overview(ctx context.Context, workspaceID string, since time.Time, top int) (OverviewData, error)
}

type DesignRepository interface {
	Create(ctx context.Context, d *Design) error
	Get(ctx context.Context, workspaceID, id string) (*Design, error)
	Update(ctx context.Context, d *Design) error
	Delete(ctx context.Context, workspaceID, id string) error
	List(ctx context.Context, workspaceID string, kind DesignKind, offset, limit int) ([]Design, int64, error)
}
