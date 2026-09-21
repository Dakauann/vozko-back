package audience

import (
	"context"
	"time"

	"vozko/domain/shared"
)

type PendingContainer struct {
	Ref         ContainerRef
	WorkspaceID string
	Pending     int
	OldestAt    time.Time
}

type Repository interface {
	Insert(ctx context.Context, a *Analysis) (inserted bool, err error)
	FindByID(ctx context.Context, workspaceID, id string) (*Analysis, error)
	FindBySourceComment(ctx context.Context, source Source, sourceCommentID string) (*Analysis, error)

	LatestBySubject(ctx context.Context, workspaceID string, source Source, kind SubjectKind, subjectID string) (*Analysis, error)

	ListPending(ctx context.Context, ref ContainerRef, limit int) ([]*Analysis, error)

	ClaimByIDs(ctx context.Context, ids []string, now time.Time) ([]string, error)

	Save(ctx context.Context, a *Analysis) error
	SaveMany(ctx context.Context, rows []*Analysis) error

	ListPendingContainers(ctx context.Context, olderThan time.Time, limit int) ([]PendingContainer, error)
	ListStaleInFlight(ctx context.Context, claimedBefore time.Time, limit int) ([]*Analysis, error)
	CountPendingBySource(ctx context.Context) (map[Source]int, error)

	List(ctx context.Context, in ListInput) (*shared.PaginatedResult[*Analysis], error)
	ListAuthorContainers(ctx context.Context, in AuthorContainersInput) (*shared.PaginatedResult[*AuthorContainer], error)
	GetStats(ctx context.Context, in ListInput) (*Stats, error)
	GetTrend(ctx context.Context, in ListInput) ([]*Rollup, error)

	AggregateAuthors(ctx context.Context, source Source, accountID string, changedSince time.Time) ([]*AuthorStats, error)
	AggregateRollups(ctx context.Context, day time.Time) ([]*Rollup, error)
	DaysAnalyzedSince(ctx context.Context, since time.Time) ([]time.Time, error)

	SoftDeleteBySourceComment(ctx context.Context, source Source, sourceCommentID string, now time.Time) error
	PurgeBefore(ctx context.Context, cutoff time.Time, limit int) (int64, error)
}

type SettingsRepository interface {
	Find(ctx context.Context, source Source, accountID string) (*Settings, error)
	Save(ctx context.Context, s *Settings) error
	ListEnabled(ctx context.Context) ([]*Settings, error)
	ListByWorkspace(ctx context.Context, workspaceID string) ([]*Settings, error)

	FindOverride(ctx context.Context, ref ContainerRef) (*ContainerOverride, error)
	SaveOverride(ctx context.Context, o *ContainerOverride) error
	DeleteOverride(ctx context.Context, ref ContainerRef) error
	ListOverrides(ctx context.Context, source Source, accountID string) ([]*ContainerOverride, error)
}

type AuthorRepository interface {
	UpsertMany(ctx context.Context, rows []*AuthorStats) error
	FindByID(ctx context.Context, workspaceID, id string) (*AuthorStats, error)
	List(ctx context.Context, in AuthorsInput) (*shared.PaginatedResult[*AuthorStats], error)
	SetModerationState(ctx context.Context, workspaceID, id string, state ModerationState, now time.Time) error
	SetRole(ctx context.Context, workspaceID, id string, role AuthorRoleInference, now time.Time) error
	ListForRoleInference(ctx context.Context, source Source, accountID string, minComments, limit int) ([]*AuthorStats, error)
}

type RollupRepository interface {
	UpsertMany(ctx context.Context, rows []*Rollup) error
	ListSeries(ctx context.Context, in TrendInput) ([]*Rollup, error)
}

type TrendRepository interface {
	GetTrend(ctx context.Context, in ListInput) ([]*Rollup, error)
}

type BatchRepository interface {
	Create(ctx context.Context, b *Batch) error
	Totals(ctx context.Context, workspaceID string, from, to time.Time) (*BatchTotals, error)
}

type BackfillRepository interface {
	Create(ctx context.Context, b *Backfill) error
	Save(ctx context.Context, b *Backfill) error
	FindByID(ctx context.Context, workspaceID, id string) (*Backfill, error)
	ClaimNextPending(ctx context.Context, now time.Time) (*Backfill, error)
	FindActive(ctx context.Context, source Source, accountID, containerID string) (*Backfill, error)
}

type AlertRuleRepository interface {
	Create(ctx context.Context, rule *AlertRule) error
	Update(ctx context.Context, rule *AlertRule) error
	Delete(ctx context.Context, workspaceID, id string) error
	FindByID(ctx context.Context, workspaceID, id string) (*AlertRule, error)
	ListByAccount(ctx context.Context, workspaceID string, source Source, accountID string) ([]*AlertRule, error)
	ListArmed(ctx context.Context, source Source, accountID string) ([]*AlertRule, error)

	ClaimFire(ctx context.Context, workspaceID, id string, now time.Time) (bool, error)

	RecordFailure(ctx context.Context, workspaceID, id, message string, now time.Time) error
}
