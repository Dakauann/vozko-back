package carriercampaign_usecase

import (
	"context"
	"time"

	cc "vozko/domain/carriercampaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/rcs"
	"vozko/domain/selection"
	"vozko/domain/shared"
	"vozko/domain/sms"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
	"vozko/usecases/campaignqueue"
	carriermessaging_usecase "vozko/usecases/carriermessaging"
)

type Selections interface {
	Selected(ctx context.Context, scope selection.Scope, s selection.Selection) (int, error)
	Freeze(ctx context.Context, scope selection.Scope, s selection.Selection, snapshotID string, pending *lead.Assignment) (lead.Frozen, error)
	Snapshot(ctx context.Context, workspaceID, snapshotID, after string, limit int) ([]string, error)
	DropSnapshot(ctx context.Context, workspaceID, snapshotID string) error
}

type Leads interface {
	FindByIDs(workspaceID string, ids []string) ([]*lead.Lead, error)
	FindByNumber(workspaceID, number string) (*lead.Lead, error)
}

type Definitions interface {
	ListByObject(workspaceID string, objectType customfield.ObjectType) ([]*customfield.Definition, error)
}

type Suppressions interface {
	SuppressedAmong(ctx context.Context, workspaceID string, phones []string) (map[string]bool, error)
}

type Senders interface {
	FindByID(ctx context.Context, workspaceID, id string) (*rcs.Sender, error)
}

type Routes interface {
	FindByID(ctx context.Context, workspaceID, id string) (*sms.Route, error)
}

type Messages interface {
	FindByID(ctx context.Context, id string) (*carriermessaging.Message, error)
}

type Deliverer interface {
	Deliver(ctx context.Context, req carriermessaging_usecase.DeliverRequest) (*carriermessaging.Message, error)
}

type SenderPauser interface {
	AdminPauseSender(ctx context.Context, id string, reason rcs.PauseReason) (*rcs.Sender, error)
}

type Settings interface {
	GetSettings(ctx context.Context, workspaceID string) (carriermessaging.Settings, error)
}

type Prices interface {
	Price(workspaceID string, kind carriermessaging.ChargeKind) (workspace_pricing.PriceResult, error)
}

type Queue interface {
	SubscribeToCampaign(runID string) error
	PauseCampaignConsumer(runID string) error
	ResumeCampaignConsumer(runID string) error
	StopCampaignConsumer(runID string) error
	IsSubscribed(runID string) bool
	SeedCounterIfMissing(runID string)
}

type Enqueuer interface {
	Enqueue(runID string, messages []campaignqueue.Message) error
}

type Attempts interface {
	Incr(key string) (int64, error)
	Del(keys ...string) error
}

type CampaignStats interface {
	FunnelsOf(ctx context.Context, workspaceID string, campaignIDs []string) (map[string]cc.Funnel, error)
}

type RecipientReader interface {
	ListRecipientRows(ctx context.Context, q RecipientQuery) (*shared.PaginatedResult[RecipientRow], error)
}

type OverviewReader interface {
	Overview(ctx context.Context, workspaceID string, from, to time.Time, tz string, channel carriermessaging.Channel) (Overview, error)
}

type WhatsAppSpend interface {
	WhatsAppCampaignSpend(ctx context.Context, workspaceID string, from, to time.Time, attributionDays int) (spendMicros, conversions int64, err error)
}
