package shortlink_usecase

import (
	"context"

	"vozko/domain/shared"
	"vozko/domain/shortlink"
)

type getShortLinkUseCase struct {
	repo shortlink.ShortLinkRepository
}

func NewGetShortLinkUseCase(repo shortlink.ShortLinkRepository) shortlink.GetShortLinkUseCase {
	return &getShortLinkUseCase{repo: repo}
}

func (uc *getShortLinkUseCase) Execute(ctx context.Context, workspaceID, id string) (*shortlink.ShortLink, error) {
	return uc.repo.FindByID(ctx, workspaceID, id)
}

type listShortLinksUseCase struct {
	repo shortlink.ShortLinkRepository
}

func NewListShortLinksUseCase(repo shortlink.ShortLinkRepository) shortlink.ListShortLinksUseCase {
	return &listShortLinksUseCase{repo: repo}
}

func (uc *listShortLinksUseCase) Execute(ctx context.Context, workspaceID string, departmentID *string, page, pageSize int) (*shared.PaginatedResult[*shortlink.ShortLink], error) {
	return uc.repo.ListByWorkspace(ctx, workspaceID, departmentID, shared.Pagination{Page: page, PageSize: pageSize})
}

type deleteShortLinkUseCase struct {
	repo   shortlink.ShortLinkRepository
	shared cacheDeleter
}

type cacheDeleter interface {
	Del(keys ...string) error
}

func NewDeleteShortLinkUseCase(repo shortlink.ShortLinkRepository, shared cacheDeleter) shortlink.DeleteShortLinkUseCase {
	return &deleteShortLinkUseCase{repo: repo, shared: shared}
}

func (uc *deleteShortLinkUseCase) Execute(ctx context.Context, workspaceID, id string) error {
	link, err := uc.repo.FindByID(ctx, workspaceID, id)
	if err != nil {
		return err
	}
	if err := uc.repo.SoftDelete(ctx, workspaceID, id); err != nil {
		return err
	}
	if uc.shared != nil {
		_ = uc.shared.Del(resolveCacheKey(link.Code))
	}
	return nil
}

type getWorkspaceStatsUseCase struct {
	repo shortlink.ShortLinkRepository
}

func NewGetWorkspaceStatsUseCase(repo shortlink.ShortLinkRepository) shortlink.GetWorkspaceStatsUseCase {
	return &getWorkspaceStatsUseCase{repo: repo}
}

func (uc *getWorkspaceStatsUseCase) Execute(ctx context.Context, workspaceID string) (*shortlink.WorkspaceClickStats, error) {
	total, err := uc.repo.CountByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	clicks, err := uc.repo.SumClicksByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return &shortlink.WorkspaceClickStats{TotalLinks: total, TotalClicks: clicks}, nil
}
