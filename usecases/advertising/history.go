package advertising

import (
	"context"

	ads "vozko/domain/advertising"
)

type historyGateway interface {
	ListActivities(ctx context.Context, token, metaAccountID string, q ads.ActivityQuery) ([]ads.AdActivity, error)
}

type HistoryUseCase struct {
	access  accountAccess
	objects ads.ObjectRepository
	gateway historyGateway
}

func NewHistoryUseCase(sync *SyncUseCase, gateway historyGateway) *HistoryUseCase {
	return &HistoryUseCase{access: sync.access, objects: sync.objects, gateway: gateway}
}

func (uc *HistoryUseCase) List(ctx context.Context, workspaceID, metaID string, dates ads.DateRange, language string) ([]ads.AdActivity, error) {
	t, err := openTarget(ctx, uc.access, uc.objects, workspaceID, metaID, ads.UseRead)
	if err != nil {
		return nil, err
	}
	loc, err := t.account.Location()
	if err != nil {
		return nil, err
	}
	if dates.Since.IsZero() {
		dates = ads.LastDays(DefaultReportDays, uc.access.now(), loc)
	}
	since, until := dates.Bounds(loc)
	activities, err := uc.gateway.ListActivities(ctx, t.token, t.account.MetaAccountID, ads.ActivityQuery{
		ObjectID: t.object.MetaID, Since: since, Until: until, Locale: ads.MetaLocale(language),
	})
	if err != nil {
		return nil, uc.access.failed(ctx, t.account, err)
	}
	return ads.NewestActivities(activities), nil
}
