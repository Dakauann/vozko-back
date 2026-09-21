package audience

import (
	"context"
	"time"

	"vozko/domain/shared"
)

type ListUseCase interface {
	Execute(ctx context.Context, in ListInput) (*shared.PaginatedResult[*Analysis], error)
}

type StatsUseCase interface {
	Execute(ctx context.Context, in ListInput) (*Stats, error)
}

type TrendsUseCase interface {
	Execute(ctx context.Context, in TrendInput) ([]*Rollup, error)
	ExecuteFiltered(ctx context.Context, in ListInput) ([]*Rollup, error)
}

type ListAuthorsUseCase interface {
	Execute(ctx context.Context, in AuthorsInput) (*shared.PaginatedResult[*AuthorStats], error)
}

type AuthorDetail struct {
	Author   *AuthorStats                       `json:"author"`
	Comments *shared.PaginatedResult[*Analysis] `json:"comments"`
}

type GetAuthorUseCase interface {
	Execute(ctx context.Context, workspaceID, authorID string, page shared.Pagination) (*AuthorDetail, error)
}

type AuthorContainers struct {
	Author     *AuthorStats                              `json:"author"`
	Containers *shared.PaginatedResult[*AuthorContainer] `json:"containers"`
}

type AuthorContainersRequest struct {
	WorkspaceID string
	AuthorID    string
	From        *time.Time
	To          *time.Time
	Page        shared.Pagination
}

type ListAuthorContainersUseCase interface {
	Execute(ctx context.Context, in AuthorContainersRequest) (*AuthorContainers, error)
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
	ReplyPolicy  *ReplyPolicy
	DailyCap     *int
	Instructions *string
}

type UpdateSettingsUseCase interface {
	Execute(ctx context.Context, in UpdateSettingsInput) (*Settings, error)
}

type RetryUseCase interface {
	Execute(ctx context.Context, workspaceID, id string) (*Analysis, error)
}

type UsageUseCase interface {
	Execute(ctx context.Context, workspaceID string) (Usage, error)
}

type UpdateWorkspaceSettingsInput struct {
	DailyCap        *int
	DebounceMinutes *int
}

type WorkspaceSettingsUseCase interface {
	Execute(ctx context.Context, workspaceID string) (WorkspaceSettings, error)
	Update(ctx context.Context, workspaceID string, in UpdateWorkspaceSettingsInput) (WorkspaceSettings, error)
}

type SpendUseCase interface {
	Execute(ctx context.Context, workspaceID string, in SpendInput) (*BatchTotals, error)
}

type SpendInput struct {
	Source    Source
	AccountID string
	Days      int
}

type BackfillEstimate struct {
	Containers        int `json:"containers"`
	EstimatedComments int `json:"estimatedComments"`
}

type StartBackfillInput struct {
	WorkspaceID       string
	Source            Source
	AccountID         string
	ContainerID       string
	RequestedByUserID string
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

type ListAccountSettingsUseCase interface {
	Execute(ctx context.Context, workspaceID string) ([]*Settings, error)
}

type ContainerSettings struct {
	Override  *ContainerOverride `json:"override,omitempty"`
	Effective Settings           `json:"effective"`
}

type GetContainerSettingsUseCase interface {
	Execute(ctx context.Context, workspaceID string, ref ContainerRef) (*ContainerSettings, error)
}

type PutContainerSettingsUseCase interface {
	Execute(ctx context.Context, o ContainerOverride) (*ContainerSettings, error)
}

type DeleteContainerSettingsUseCase interface {
	Execute(ctx context.Context, workspaceID string, ref ContainerRef) (*ContainerSettings, error)
}

type EscalateCommentInput struct {
	WorkspaceID   string
	UserID        string
	CommentID     string
	RecipientID   string
	RecipientKind string
	Note          string
}

type EscalateCommentUseCase interface {
	Execute(ctx context.Context, in EscalateCommentInput) (*Escalation, error)
}

type SuggestReplyInput struct {
	WorkspaceID string
	CommentID   string
}

type SuggestCommentReplyUseCase interface {
	Execute(ctx context.Context, in SuggestReplyInput) (*ReplySuggestion, error)
}

type PostReplyInput struct {
	WorkspaceID string
	UserID      string
	CommentID   string
	Text        string
}

type PostCommentReplyUseCase interface {
	Execute(ctx context.Context, in PostReplyInput) (*ReplySuggestion, error)
}
