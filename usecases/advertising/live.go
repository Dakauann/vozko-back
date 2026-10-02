package advertising

import (
	"context"
	"fmt"

	ads "vozko/domain/advertising"
)

type insightsGateway interface {
	LiveInsights(ctx context.Context, token, metaAccountID string, q ads.LiveQuery) ([]ads.LiveRow, error)
}

type LiveReport struct {
	Account *ads.AdAccount
	Query   ads.LiveQuery
	Rows    []ads.LiveRow
}

type LiveUseCase struct {
	access  accountAccess
	gateway insightsGateway
}

func NewLiveUseCase(sync *SyncUseCase, gateway insightsGateway) *LiveUseCase {
	return &LiveUseCase{access: sync.access, gateway: gateway}
}

func (uc *LiveUseCase) Insights(ctx context.Context, q ads.LiveQuery) (*LiveReport, error) {
	q.Level = q.Level.OrCampaign()
	account, token, err := uc.access.open(ctx, q.WorkspaceID, q.AccountID, ads.ScopeAdsRead)
	if err != nil {
		return nil, err
	}
	if q.Range.Since.IsZero() {
		loc, err := account.Location()
		if err != nil {
			return nil, err
		}
		q.Range = ads.LastDays(DefaultReportDays, uc.access.now(), loc)
	}
	if err := q.Validate(); err != nil {
		return nil, err
	}
	currency, err := ads.NormalizeCurrency(account.Currency)
	if err != nil {
		return nil, err
	}
	rows, err := uc.gateway.LiveInsights(ctx, token, account.MetaAccountID, q)
	if err != nil {
		return nil, uc.access.failed(ctx, account, err)
	}
	for _, row := range rows {
		if row.Values.Currency != "" && row.Values.Currency != currency {
			return nil, fmt.Errorf("%w: live insight in %q for an account in %s", ads.ErrMixedCurrencies, row.Values.Currency, currency)
		}
	}
	return &LiveReport{Account: account, Query: q, Rows: rows}, nil
}
