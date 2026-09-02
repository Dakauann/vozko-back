package comment_analysis

import (
	"context"
	"time"

	"vozko/domain/shared"
)

// PendingContainer is one container the backstop found with work waiting,
// from the database alone.
type PendingContainer struct {
	Ref         ContainerRef
	WorkspaceID string
	Pending     int
	OldestAt    time.Time
}

// Repository persists comment analyses.
//
// Insert and ClaimPending are the load-bearing half of the contract.
// Insert must be ON CONFLICT DO NOTHING on (source, source_comment_id):
// that one clause is what makes webhook redelivery free. ClaimPending must
// be a single conditional write (UPDATE … WHERE status = 'pending' …
// RETURNING); an implementation that reads then writes would let two ticks
// both observe pending and both send the same comment to the model.
type Repository interface {
	// Insert stores a new row and reports whether it was actually inserted;
	// false with a nil error means a row for that source comment already
	// existed.
	Insert(ctx context.Context, a *CommentAnalysis) (inserted bool, err error)
	FindByID(ctx context.Context, workspaceID, id string) (*CommentAnalysis, error)
	FindBySourceComment(ctx context.Context, source Source, sourceCommentID string) (*CommentAnalysis, error)

	// ListPending returns up to limit pending rows of the container, oldest
	// first, WITHOUT claiming them. The flush job plans batches from this
	// and claims only what it is about to send, so a row the cycle ceiling
	// defers never has an attempt counted against it.
	ListPending(ctx context.Context, ref ContainerRef, limit int) ([]*CommentAnalysis, error)

	// ClaimByIDs flips the given rows from pending to in_flight (attempts+1,
	// updated_at=now) in one conditional write and returns the ids it
	// actually claimed. A row another replica took first is simply absent
	// from the result; concurrent callers receive disjoint sets.
	ClaimByIDs(ctx context.Context, ids []string, now time.Time) ([]string, error)

	// Save and SaveMany persist state the domain already computed (Apply,
	// Release, Fail, Retry, SoftDelete).
	Save(ctx context.Context, a *CommentAnalysis) error
	SaveMany(ctx context.Context, rows []*CommentAnalysis) error

	// ListPendingContainers is the backstop (§6.3): containers holding
	// pending rows created before olderThan, found without Redis.
	ListPendingContainers(ctx context.Context, olderThan time.Time, limit int) ([]PendingContainer, error)
	// ListStaleInFlight returns rows claimed before claimedBefore that never
	// resolved, because a replica died mid-flight (§8).
	ListStaleInFlight(ctx context.Context, claimedBefore time.Time, limit int) ([]*CommentAnalysis, error)
	// CountPendingBySource feeds the comment_analysis_pending gauge.
	CountPendingBySource(ctx context.Context) (map[Source]int, error)

	List(ctx context.Context, in ListInput) (*shared.PaginatedResult[*CommentAnalysis], error)
	// GetStats fills Counters and Topics with COUNT(*) FILTER; the use case
	// calls Stats.Finalize.
	GetStats(ctx context.Context, in ListInput) (*Stats, error)

	// AggregateAuthors computes the per-author counters for one account
	// from rows analysed since changedSince. Derivation (stance, flag) is
	// the domain's; the repository only counts.
	AggregateAuthors(ctx context.Context, source Source, accountID string, changedSince time.Time) ([]*AuthorStats, error)
	// AggregateRollups computes every (scope, scopeID) counter set for one
	// UTC day (by CommentedAt) across all accounts. Soft-deleted rows are
	// included: a historical rollup must not change because a comment was
	// deleted later.
	AggregateRollups(ctx context.Context, day time.Time) ([]*Rollup, error)
	// DaysAnalyzedSince returns the distinct UTC days (by CommentedAt) that
	// received a classification since the cutoff, so the rollup job rebuilds
	// exactly the buckets a backfill or a late flush touched.
	DaysAnalyzedSince(ctx context.Context, since time.Time) ([]time.Time, error)

	SoftDeleteBySourceComment(ctx context.Context, source Source, sourceCommentID string, now time.Time) error
	// PurgeBefore deletes rows created before cutoff, at most limit per call
	// so retention never holds a long lock.
	PurgeBefore(ctx context.Context, cutoff time.Time, limit int) (int64, error)
}

// SettingsRepository stores the per-account configuration.
type SettingsRepository interface {
	// Find returns ErrNotFound for an account that was never configured;
	// callers fall back to NewSettings (disabled).
	Find(ctx context.Context, source Source, accountID string) (*Settings, error)
	Save(ctx context.Context, s *Settings) error
	// ListEnabled returns every account with analysis switched on.
	ListEnabled(ctx context.Context) ([]*Settings, error)
	// ListByWorkspace returns every configured account of a workspace, for
	// the workspace-wide dashboard's account picker.
	ListByWorkspace(ctx context.Context, workspaceID string) ([]*Settings, error)

	// Container overrides: a post's own settings, layered over the account's.
	FindOverride(ctx context.Context, ref ContainerRef) (*ContainerOverride, error)
	SaveOverride(ctx context.Context, o *ContainerOverride) error
	DeleteOverride(ctx context.Context, ref ContainerRef) error
	// ListOverrides returns an account's overrides, so a UI can mark which
	// posts carry their own settings.
	ListOverrides(ctx context.Context, source Source, accountID string) ([]*ContainerOverride, error)
}

// AuthorRepository stores the author projection.
type AuthorRepository interface {
	// UpsertMany writes rows keyed on (source, account, author). It must
	// PRESERVE ModerationState on an existing row: the rollup job rebuilds
	// counters, not operator decisions.
	UpsertMany(ctx context.Context, rows []*AuthorStats) error
	FindByID(ctx context.Context, workspaceID, id string) (*AuthorStats, error)
	List(ctx context.Context, in AuthorsInput) (*shared.PaginatedResult[*AuthorStats], error)
	SetModerationState(ctx context.Context, workspaceID, id string, state ModerationState, now time.Time) error
}

// RollupRepository stores the daily snapshots.
type RollupRepository interface {
	UpsertMany(ctx context.Context, rows []*Rollup) error
	// ListSeries returns the scope's rows in [From, To], ascending by day.
	ListSeries(ctx context.Context, in TrendInput) ([]*Rollup, error)
}

// BatchRepository stores the per-call receipts.
type BatchRepository interface {
	Create(ctx context.Context, b *Batch) error
	// Totals sums a workspace's batches in [from, to).
	Totals(ctx context.Context, workspaceID string, from, to time.Time) (*BatchTotals, error)
}

// BackfillRepository stores backfill runs.
type BackfillRepository interface {
	Create(ctx context.Context, b *Backfill) error
	Save(ctx context.Context, b *Backfill) error
	FindByID(ctx context.Context, workspaceID, id string) (*Backfill, error)
	// ClaimNextPending flips one pending backfill to running and returns it,
	// or (nil, nil) when there is none. One conditional write, like
	// ClaimPending.
	ClaimNextPending(ctx context.Context, now time.Time) (*Backfill, error)
	// FindActive returns the non-terminal backfill for a container (or the
	// account when containerID is empty), so two cannot run at once.
	FindActive(ctx context.Context, source Source, accountID, containerID string) (*Backfill, error)
}
