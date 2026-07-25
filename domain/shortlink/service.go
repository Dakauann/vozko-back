package shortlink

import (
	"context"
	"time"

	"vozko/domain/shared"
)

type ResolveState string

const (
	ResolveOK       ResolveState = "ok"
	ResolvePassword ResolveState = "password_required"
	ResolveNotFound ResolveState = "not_found"
	ResolveGone     ResolveState = "gone"
)

type ResolvedLink struct {
	State        ResolveState `json:"state"`
	ShortLinkID  string       `json:"shortLinkId"`
	WorkspaceID  string       `json:"workspaceId"`
	Code         string       `json:"code"`
	TargetURL    string       `json:"targetUrl"`
	RedirectType RedirectType `json:"redirectType"`
	HasPassword  bool         `json:"hasPassword"`
}

type CreateShortLinkInput struct {
	WorkspaceID  string
	DepartmentID *string
	CreatedBy    string
	TargetURL    string
	CustomAlias  string
	Title        string
	RedirectType string
	Password     string
	ExpiresAt    *time.Time
	MaxClicks    *int64
}

type UpdateShortLinkInput struct {
	TargetURL      *string
	Title          *string
	RedirectType   *string
	Status         *string
	Password       *string
	ExpiresAt      *time.Time
	MaxClicks      *int64
	ClearPassword  bool
	ClearExpiry    bool
	ClearMaxClicks bool
}

type CreateShortLinkUseCase interface {
	Execute(ctx context.Context, input CreateShortLinkInput) (*ShortLink, error)
}

type UpdateShortLinkUseCase interface {
	Execute(ctx context.Context, workspaceID, id string, input UpdateShortLinkInput) (*ShortLink, error)
}

type GetShortLinkUseCase interface {
	Execute(ctx context.Context, workspaceID, id string) (*ShortLink, error)
}

type ListShortLinksUseCase interface {
	Execute(ctx context.Context, workspaceID string, departmentID *string, page, pageSize int) (*shared.PaginatedResult[*ShortLink], error)
}

type DeleteShortLinkUseCase interface {
	Execute(ctx context.Context, workspaceID, id string) error
}

type ResolveShortLinkUseCase interface {
	Execute(ctx context.Context, code string) (*ResolvedLink, error)
}

type UnlockShortLinkUseCase interface {
	Execute(ctx context.Context, code, password string) (*ResolvedLink, error)
}

type PublishClickUseCase interface {
	Execute(ctx context.Context, msg ClickMessage) error
}

type ConsumeClickUseCase interface {
	Start() error
}

type GetAnalyticsUseCase interface {
	Execute(ctx context.Context, input AnalyticsInput) (*Analytics, error)
}

type ListRecentClicksUseCase interface {
	Execute(ctx context.Context, workspaceID, shortLinkID string, page, pageSize int) (*shared.PaginatedResult[*Click], error)
}

type GenerateQRUseCase interface {
	Execute(ctx context.Context, workspaceID, id string, size int) ([]byte, error)
}

type PurgeClicksUseCase interface {
	Execute(ctx context.Context) error
}

type GetWorkspaceStatsUseCase interface {
	Execute(ctx context.Context, workspaceID string) (*WorkspaceClickStats, error)
}

type WorkspaceClickStats struct {
	TotalLinks  int   `json:"totalLinks"`
	TotalClicks int64 `json:"totalClicks"`
}
