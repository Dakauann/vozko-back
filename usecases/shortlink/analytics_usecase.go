package shortlink_usecase

import (
	"context"
	"time"

	"vozko/domain/shared"
	"vozko/domain/shortlink"
)

const defaultAnalyticsWindowDays = 30

type getAnalyticsUseCase struct {
	clickRepo shortlink.ClickRepository
}

func NewGetAnalyticsUseCase(clickRepo shortlink.ClickRepository) shortlink.GetAnalyticsUseCase {
	return &getAnalyticsUseCase{clickRepo: clickRepo}
}

func (uc *getAnalyticsUseCase) Execute(ctx context.Context, input shortlink.AnalyticsInput) (*shortlink.Analytics, error) {
	if input.To.IsZero() {
		input.To = time.Now()
	}
	if input.From.IsZero() {
		input.From = input.To.AddDate(0, 0, -defaultAnalyticsWindowDays)
	}
	return uc.clickRepo.Analytics(ctx, input)
}

type listRecentClicksUseCase struct {
	clickRepo shortlink.ClickRepository
}

func NewListRecentClicksUseCase(clickRepo shortlink.ClickRepository) shortlink.ListRecentClicksUseCase {
	return &listRecentClicksUseCase{clickRepo: clickRepo}
}

func (uc *listRecentClicksUseCase) Execute(ctx context.Context, workspaceID, shortLinkID string, page, pageSize int) (*shared.PaginatedResult[*shortlink.Click], error) {
	return uc.clickRepo.RecentClicks(ctx, workspaceID, shortLinkID, shared.Pagination{Page: page, PageSize: pageSize})
}
