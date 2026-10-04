package advertisinghttp

import (
	"errors"
	"time"

	"vozko/domain/advertising"
)

func presentAccount(a *advertising.AdAccount) AccountResponse {
	out := AccountResponse{
		ID: a.ID, MetaAccountID: a.MetaAccountID, Name: a.Name, BusinessName: a.BusinessName,
		Currency: a.Currency, Timezone: a.Timezone, MetaStatus: a.MetaStatus.Key(),
		Connection: string(a.Connection), HasFunding: a.HasFunding, AmountSpent: a.AmountSpent, SpendCap: a.SpendCapLimit(),
		LastSyncedAt: a.LastSyncedAt,
	}
	out.Role = string(a.Role())
	out.CanManage = a.CanManage() == nil
	out.CanSetSpendCap = a.CanChangeBilling() == nil
	if err := a.CanSpend(); err != nil {
		out.SpendBlocker = spendBlocker(err)
	} else {
		out.CanSpend = true
	}
	return out
}

func spendBlocker(err error) string {
	for _, m := range domainErrors {
		if errors.Is(err, m.target) {
			return m.code
		}
	}
	return "unknown"
}

func presentMetrics(m advertising.Metrics, currency string) MetricsResponse {
	if m.Currency != "" {
		currency = m.Currency
	}
	return MetricsResponse{
		Currency: currency, Spend: m.SpendMicros, Impressions: m.Impressions, Clicks: m.Clicks,
		LinkClicks: m.LinkClicks, Results: m.Results, ResultAction: m.ResultAction, MixedResults: m.MixedResults,
		CostPerResult: m.CostPerResult(), Conversations: m.Conversations, CostPerConversation: m.CostPerConversation(),
		CPC: m.CostPerLinkClick(), CPM: m.CPM(), CTR: m.CTR(),
	}
}

func presentOutcome(o advertising.Outcome) OutcomeResponse {
	return OutcomeResponse{
		Conversations: o.Conversations, Leads: o.Leads, WonDeals: o.WonDeals, Revenue: o.RevenueMicros,
		CostPerConversation: o.CostPerConversation, CostPerLead: o.CostPerLead, ROAS: o.ROAS,
	}
}

func presentObject(o *advertising.Object, now time.Time) RowResponse {
	row := RowResponse{
		MetaID: o.MetaID, Level: string(o.Level), Name: o.Name, CampaignID: o.CampaignMetaID, AdSetID: o.AdSetMetaID,
		Status: string(o.Status), EffectiveStatus: string(o.EffectiveStatus), Delivery: string(o.Delivery(now)), Delivered: o.Delivered(),
		IsOn: o.IsOn(), CanToggle: o.CanToggle() == nil, Objective: o.Objective, OptimizationGoal: o.OptimizationGoal,
		DestinationType: o.DestinationType, DailyBudget: o.DailyBudget, LifetimeBudget: o.LifetimeBudget,
		StartTime: o.StartTime, EndTime: o.EndTime, Issues: o.Issues, ReviewFeedback: o.ReviewFeedback,
	}
	if row.Issues == nil {
		row.Issues = []advertising.Issue{}
	}
	if o.Creative != nil {
		row.Creative = &CreativeResponse{Title: o.Creative.Title, Body: o.Creative.Body, ImageURL: o.Creative.ImageURL, ThumbnailURL: o.Creative.ThumbnailURL}
	}
	return row
}

func presentRange(r advertising.DateRange) RangeResponse {
	return RangeResponse{Since: r.Since.Format(advertising.DayLayout), Until: r.Until.Format(advertising.DayLayout)}
}

func presentBudgetMinimum(m *advertising.BudgetMinimum) *BudgetMinimumResponse {
	if m == nil {
		return nil
	}
	return &BudgetMinimumResponse{Field: m.Field, Daily: m.Daily, Currency: m.Currency}
}
