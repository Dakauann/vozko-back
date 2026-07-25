package shortlink

import (
	"context"
	"time"

	"vozko/domain/shared"
)

type ShortLinkRepository interface {
	Create(ctx context.Context, link *ShortLink) error
	Update(ctx context.Context, link *ShortLink) error
	SoftDelete(ctx context.Context, workspaceID, id string) error
	FindByID(ctx context.Context, workspaceID, id string) (*ShortLink, error)
	FindByCode(ctx context.Context, code string) (*ShortLink, error)
	CodeExists(ctx context.Context, code string) (bool, error)
	ListByWorkspace(ctx context.Context, workspaceID string, departmentID *string, opts shared.Pagination) (*shared.PaginatedResult[*ShortLink], error)
	CountByWorkspace(ctx context.Context, workspaceID string) (int, error)
	SumClicksByWorkspace(ctx context.Context, workspaceID string) (int64, error)
	ApplyClick(ctx context.Context, id string, uniqueDelta int64, occurredAt time.Time) error
}

type ClickRepository interface {
	RecordClick(ctx context.Context, click *Click) (bool, error)
	ApplyDailyStats(ctx context.Context, deltas []DailyStatDelta) error
	Analytics(ctx context.Context, in AnalyticsInput) (*Analytics, error)
	RecentClicks(ctx context.Context, workspaceID, shortLinkID string, opts shared.Pagination) (*shared.PaginatedResult[*Click], error)
	PurgeClicksBefore(ctx context.Context, cutoff time.Time) (int64, error)
	PurgeDailyStatsBefore(ctx context.Context, cutoff time.Time) (int64, error)
}
