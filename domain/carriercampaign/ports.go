package carriercampaign

import (
	"context"
	"time"

	"vozko/domain/campaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/rcs"
	"vozko/domain/shared"
)

var QueueNamespace = campaign.Namespace{
	Topic: "carrier_campaign_dispatch",
	Key:   "campaign:carrier",
}

type ListCampaignsInput struct {
	WorkspaceID string
	Channel     carriermessaging.Channel
	Status      Status
	Kind        Kind
	SenderID    string
	Search      string
	Pagination  shared.Pagination
}

type CampaignRepository interface {
	campaign.ScheduleClaimer
	Create(ctx context.Context, c *Campaign) error
	Update(ctx context.Context, c *Campaign, from Status) error
	Delete(ctx context.Context, workspaceID, id string) error
	FindByID(ctx context.Context, workspaceID, id string) (*Campaign, error)
	FindByIDUnscoped(ctx context.Context, id string) (*Campaign, error)
	List(ctx context.Context, in ListCampaignsInput) (*shared.PaginatedResult[*Campaign], error)
	ListByStatus(ctx context.Context, statuses ...Status) ([]*Campaign, error)
	ListScheduledDue(ctx context.Context, now time.Time, limit int) ([]*Campaign, error)
	SwapStatus(ctx context.Context, id string, from, to Status) (bool, error)
	Reschedule(ctx context.Context, id string, from, to Status, at *time.Time) (bool, error)
	CountByStatus(ctx context.Context, workspaceID string) (map[Status]int64, error)
}

type RunRepository interface {
	Create(ctx context.Context, r *Run) (bool, error)
	FindByID(ctx context.Context, id string) (*Run, error)
	FindByOccurrence(ctx context.Context, campaignID string, occurrence time.Time) (*Run, error)
	FindActive(ctx context.Context, campaignID string) (*Run, error)
	ListByCampaign(ctx context.Context, campaignID string, page shared.Pagination) (*shared.PaginatedResult[*Run], error)
	ListByStatus(ctx context.Context, statuses ...campaign.Status) ([]*Run, error)
	SwapStatus(ctx context.Context, id string, from, to campaign.Status) (bool, error)
	Complete(ctx context.Context, id string, at time.Time) (bool, error)
	CountByStatus(ctx context.Context, campaignID string) (map[campaign.Status]int64, error)
}

type ListRecipientsInput struct {
	WorkspaceID string
	RunID       string
	Status      RecipientStatus
	SkipReason  SkipReason
	Variant     *int
	Search      string
	Pagination  shared.Pagination
}

type RecipientRepository interface {
	CreateMany(ctx context.Context, recipients []Recipient) error
	FindByID(ctx context.Context, id string) (*Recipient, error)
	FindByMessageID(ctx context.Context, messageID string) (*Recipient, error)
	MarkSent(ctx context.Context, id, messageID string, at time.Time) (bool, error)
	MarkSkipped(ctx context.Context, id string, reason SkipReason) (bool, error)
	MarkFailed(ctx context.Context, id, errorCode string) (bool, error)
	PendingIDs(ctx context.Context, runID, after string, limit int) ([]string, error)
	List(ctx context.Context, in ListRecipientsInput) (*shared.PaginatedResult[*Recipient], error)
	Counts(ctx context.Context, runID string) (RecipientCounts, error)
	CountsByVariant(ctx context.Context, runID string) (map[int]RecipientCounts, error)
}

type FunnelQuery struct {
	WorkspaceID string
	Channel     carriermessaging.Channel
	CampaignID  string
	RunID       string
}

type FunnelReader interface {
	Funnel(ctx context.Context, q FunnelQuery) (Funnel, error)
	VariantFunnels(ctx context.Context, q FunnelQuery) (map[int]Funnel, error)
	ButtonCounts(ctx context.Context, q FunnelQuery, variant int) (taps, clicks map[rcs.ButtonKey]int64, err error)
	Carriers(ctx context.Context, q FunnelQuery) ([]CarrierStat, error)
	Touches(ctx context.Context, workspaceID, leadID string, since time.Time) ([]Touch, error)
}
