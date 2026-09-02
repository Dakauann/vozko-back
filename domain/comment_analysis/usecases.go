package comment_analysis

import (
	"context"

	"vozko/domain/shared"
)

// Use-case ports the delivery layer depends on. Every read is scoped to the
// caller's workspace INSIDE the use case, never from a query parameter,
// which is the posture every existing handler takes.

type ListUseCase interface {
	Execute(ctx context.Context, in ListInput) (*shared.PaginatedResult[*CommentAnalysis], error)
}

type StatsUseCase interface {
	Execute(ctx context.Context, in ListInput) (*Stats, error)
}

type TrendsUseCase interface {
	Execute(ctx context.Context, in TrendInput) ([]*Rollup, error)
}

type ListAuthorsUseCase interface {
	Execute(ctx context.Context, in AuthorsInput) (*shared.PaginatedResult[*AuthorStats], error)
}

// AuthorDetail is one author plus their comments: the row a flagged-authors
// table expands into.
type AuthorDetail struct {
	Author   *AuthorStats                              `json:"author"`
	Comments *shared.PaginatedResult[*CommentAnalysis] `json:"comments"`
}

type GetAuthorUseCase interface {
	Execute(ctx context.Context, workspaceID, authorID string, page shared.Pagination) (*AuthorDetail, error)
}

type SetModerationStateInput struct {
	WorkspaceID string
	AuthorID    string
	State       ModerationState
}

type SetModerationStateUseCase interface {
	Execute(ctx context.Context, in SetModerationStateInput) (*AuthorStats, error)
}

type GetSettingsUseCase interface {
	// Execute returns the stored settings or, for an account never
	// configured, the disabled defaults for its vertical.
	Execute(ctx context.Context, workspaceID string, source Source, accountID string) (*Settings, error)
}

type UpdateSettingsInput struct {
	WorkspaceID string
	Source      Source
	AccountID   string

	Enabled      *bool
	Model        *string
	Vertical     *Vertical
	Topics       *TopicSet
	ActionPolicy *ActionPolicy
	DailyCap     *int
	Instructions *string
}

type UpdateSettingsUseCase interface {
	Execute(ctx context.Context, in UpdateSettingsInput) (*Settings, error)
}

// RetryUseCase re-queues a failed row (§8: never silently dropped).
type RetryUseCase interface {
	Execute(ctx context.Context, workspaceID, id string) (*CommentAnalysis, error)
}

// SpendUseCase is the §9.4 receipt: what the workspace bought this period.
type SpendUseCase interface {
	Execute(ctx context.Context, workspaceID string, in SpendInput) (*BatchTotals, error)
}

type SpendInput struct {
	Source    Source
	AccountID string
	// Days back from now; 0 means the current calendar month.
	Days int
}

// ---- Backfill (§10) ----

type BackfillEstimate struct {
	Containers        int   `json:"containers"`
	EstimatedComments int   `json:"estimatedComments"`
	EstimatedMicros   int64 `json:"estimatedMicros"`
}

type StartBackfillInput struct {
	WorkspaceID       string
	Source            Source
	AccountID         string
	ContainerID       string // empty: the whole account
	RequestedByUserID string
	// ConfirmedEstimate must match what the estimate endpoint returned;
	// silently charging for a 100k-comment backfill is a support incident.
	ConfirmedEstimate int
}

type EstimateBackfillUseCase interface {
	Execute(ctx context.Context, workspaceID string, source Source, accountID, containerID string) (*BackfillEstimate, error)
}

type StartBackfillUseCase interface {
	Execute(ctx context.Context, in StartBackfillInput) (*Backfill, error)
}

type GetBackfillUseCase interface {
	Execute(ctx context.Context, workspaceID, id string) (*Backfill, error)
}

type CancelBackfillUseCase interface {
	Execute(ctx context.Context, workspaceID, id string) (*Backfill, error)
}

// ---- Workspace-wide and per-post settings ----

// ListAccountSettingsUseCase lists every configured account of the caller's
// workspace (the audience dashboard's picker).
type ListAccountSettingsUseCase interface {
	Execute(ctx context.Context, workspaceID string) ([]*Settings, error)
}

// ContainerSettings is what the post-level editor shows: the override as
// stored (nil when the post inherits everything) and the effective result.
type ContainerSettings struct {
	Override  *ContainerOverride `json:"override,omitempty"`
	Effective Settings           `json:"effective"`
}

type GetContainerSettingsUseCase interface {
	Execute(ctx context.Context, workspaceID string, ref ContainerRef) (*ContainerSettings, error)
}

// PutContainerSettingsUseCase replaces a post's override. An override that
// changes nothing is deleted rather than stored.
type PutContainerSettingsUseCase interface {
	Execute(ctx context.Context, o ContainerOverride) (*ContainerSettings, error)
}

type DeleteContainerSettingsUseCase interface {
	Execute(ctx context.Context, workspaceID string, ref ContainerRef) (*ContainerSettings, error)
}
