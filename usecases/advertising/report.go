package advertising

import (
	"context"
	"slices"
	"sort"
	"time"

	ads "vozko/domain/advertising"
)

type ReportQuery struct {
	WorkspaceID string
	AccountID   string
	Level       ads.Level
	Range       ads.DateRange
	CampaignIDs []string
	AdSetIDs    []string
	ObjectIDs   []string
	Search      string
	Compare     bool
}

type ReportRow struct {
	Object  *ads.Object
	Metrics ads.Metrics
	Outcome ads.Outcome
}

type Report struct {
	Account  *ads.AdAccount
	Range    ads.DateRange
	Level    ads.Level
	Totals   ads.Metrics
	Outcome  ads.Outcome
	Rows     []ReportRow
	Previous *Period
}

type Period struct {
	Range   ads.DateRange
	Totals  ads.Metrics
	Outcome ads.Outcome
}

type TrendPoint struct {
	Day     time.Time
	Metrics ads.Metrics
}

type Trend struct {
	Account *ads.AdAccount
	Range   ads.DateRange
	Points  []TrendPoint
}

const DefaultReportDays = 30

type ReportUseCase struct {
	now         func() time.Time
	accounts    ads.AccountRepository
	objects     ads.ObjectRepository
	insights    ads.InsightRepository
	attribution ads.AttributionRepository
}

func NewReportUseCase(accounts ads.AccountRepository, objects ads.ObjectRepository, insights ads.InsightRepository, attribution ads.AttributionRepository) *ReportUseCase {
	return &ReportUseCase{now: func() time.Time { return time.Now().UTC() }, accounts: accounts, objects: objects, insights: insights, attribution: attribution}
}

type adOwner struct {
	campaignID string
	adSetID    string
}

type accountData struct {
	account *ads.AdAccount
	dates   ads.DateRange
	goals   map[string]string
	owners  map[string]adOwner
	rows    []ads.DailyInsight
}

func (uc *ReportUseCase) load(ctx context.Context, workspaceID, accountID string, r ads.DateRange) (*accountData, error) {
	account, err := uc.accounts.FindByID(ctx, workspaceID, accountID)
	if err != nil {
		return nil, err
	}
	if r.Since.IsZero() && r.Until.IsZero() {
		loc, err := account.Location()
		if err != nil {
			return nil, err
		}
		r = ads.LastDays(DefaultReportDays, uc.now(), loc)
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if _, err := ads.NormalizeCurrency(account.Currency); err != nil {
		return nil, err
	}
	adSets, err := uc.objects.List(ctx, ads.ObjectQuery{WorkspaceID: workspaceID, AdAccountID: account.ID, Level: ads.LevelAdSet, IncludeRemoved: true})
	if err != nil {
		return nil, err
	}
	adObjects, err := uc.objects.List(ctx, ads.ObjectQuery{WorkspaceID: workspaceID, AdAccountID: account.ID, Level: ads.LevelAd, IncludeRemoved: true})
	if err != nil {
		return nil, err
	}
	rows, err := uc.insights.Rows(ctx, account.ID, r)
	if err != nil {
		return nil, err
	}
	data := &accountData{account: account, dates: r, goals: map[string]string{}, owners: map[string]adOwner{}, rows: rows}
	for _, s := range adSets {
		data.goals[s.MetaID] = s.OptimizationGoal
	}
	for _, a := range adObjects {
		data.owners[a.MetaID] = adOwner{campaignID: a.CampaignMetaID, adSetID: a.AdSetMetaID}
	}
	for _, row := range rows {
		if _, known := data.owners[row.AdMetaID]; !known {
			data.owners[row.AdMetaID] = adOwner{campaignID: row.CampaignMetaID, adSetID: row.AdSetMetaID}
		}
	}
	return data, nil
}

func inScope(owner adOwner, campaignIDs, adSetIDs []string) bool {
	if len(campaignIDs) > 0 && !slices.Contains(campaignIDs, owner.campaignID) {
		return false
	}
	if len(adSetIDs) > 0 && !slices.Contains(adSetIDs, owner.adSetID) {
		return false
	}
	return true
}

func keyFor(level ads.Level, adID string, owner adOwner) string {
	switch level {
	case ads.LevelCampaign:
		return owner.campaignID
	case ads.LevelAdSet:
		return owner.adSetID
	}
	return adID
}

func (uc *ReportUseCase) Report(ctx context.Context, q ReportQuery) (*Report, error) {
	q.Level = q.Level.OrCampaign()
	if !q.Level.Valid() {
		return nil, ads.FieldError("level", "invalid")
	}
	data, err := uc.load(ctx, q.WorkspaceID, q.AccountID, q.Range)
	if err != nil {
		return nil, err
	}
	listed, err := uc.objects.List(ctx, ads.ObjectQuery{
		WorkspaceID: q.WorkspaceID, AdAccountID: data.account.ID, Level: q.Level,
		CampaignIDs: q.CampaignIDs, AdSetIDs: q.AdSetIDs, MetaIDs: q.ObjectIDs, Search: q.Search,
	})
	if err != nil {
		return nil, err
	}
	metrics := map[string][]ads.Metrics{}
	for _, row := range data.rows {
		owner := data.owners[row.AdMetaID]
		if !inScope(owner, q.CampaignIDs, q.AdSetIDs) {
			continue
		}
		key := keyFor(q.Level, row.AdMetaID, owner)
		metrics[key] = append(metrics[key], ads.MetricsOf(row, data.goals[owner.adSetID]))
	}
	attributions, err := uc.attributionByKey(ctx, q, data)
	if err != nil {
		return nil, err
	}
	report := &Report{Account: data.account, Range: data.dates, Level: q.Level}
	var shownMetrics []ads.Metrics
	var shownAttributions []ads.Attribution
	for _, object := range listed {
		sum, err := ads.SumMetrics(metrics[object.MetaID])
		if err != nil {
			return nil, err
		}
		attribution := ads.SumAttributions(attributions[object.MetaID])
		report.Rows = append(report.Rows, ReportRow{Object: object, Metrics: sum, Outcome: ads.OutcomeOf(sum, attribution)})
		shownMetrics = append(shownMetrics, sum)
		shownAttributions = append(shownAttributions, attribution)
	}
	totals, err := ads.SumMetrics(shownMetrics)
	if err != nil {
		return nil, err
	}
	report.Totals = totals
	report.Outcome = ads.OutcomeOf(totals, ads.SumAttributions(shownAttributions))
	if q.Compare {
		previous := q
		previous.Compare, previous.Range = false, ads.PreviousRange(report.Range)
		before, err := uc.Report(ctx, previous)
		if err != nil {
			return nil, err
		}
		report.Previous = &Period{Range: before.Range, Totals: before.Totals, Outcome: before.Outcome}
	}
	return report, nil
}

type ObjectReport struct {
	Account *ads.AdAccount
	Range   ads.DateRange
	Row     ReportRow
}

func (uc *ReportUseCase) ObjectRow(ctx context.Context, workspaceID, metaID string, r ads.DateRange) (*ObjectReport, error) {
	object, err := uc.objects.Find(ctx, workspaceID, metaID)
	if err != nil {
		return nil, err
	}
	report, err := uc.Report(ctx, ReportQuery{
		WorkspaceID: workspaceID, AccountID: object.AdAccountID, Level: object.Level, Range: r, ObjectIDs: []string{object.MetaID},
	})
	if err != nil {
		return nil, err
	}
	if len(report.Rows) != 1 {
		return nil, ads.ErrObjectNotFound
	}
	return &ObjectReport{Account: report.Account, Range: report.Range, Row: report.Rows[0]}, nil
}

func (uc *ReportUseCase) attributionByKey(ctx context.Context, q ReportQuery, data *accountData) (map[string][]ads.Attribution, error) {
	loc, err := data.account.Location()
	if err != nil {
		return nil, err
	}
	adIDs := make([]string, 0, len(data.owners))
	for id, owner := range data.owners {
		if inScope(owner, q.CampaignIDs, q.AdSetIDs) {
			adIDs = append(adIDs, id)
		}
	}
	sort.Strings(adIDs)
	from, to := data.dates.Bounds(loc)
	rows, err := uc.attribution.ByAd(ctx, q.WorkspaceID, adIDs, from, to)
	if err != nil {
		return nil, err
	}
	out := map[string][]ads.Attribution{}
	for _, a := range rows {
		key := keyFor(q.Level, a.AdMetaID, data.owners[a.AdMetaID])
		out[key] = append(out[key], a)
	}
	return out, nil
}

func (uc *ReportUseCase) Trend(ctx context.Context, q ReportQuery) (*Trend, error) {
	data, err := uc.load(ctx, q.WorkspaceID, q.AccountID, q.Range)
	if err != nil {
		return nil, err
	}
	byDay := map[string][]ads.Metrics{}
	for _, row := range data.rows {
		owner := data.owners[row.AdMetaID]
		if !inScope(owner, q.CampaignIDs, q.AdSetIDs) {
			continue
		}
		byDay[row.Day.Format(ads.DayLayout)] = append(byDay[row.Day.Format(ads.DayLayout)], ads.MetricsOf(row, data.goals[owner.adSetID]))
	}
	trend := &Trend{Account: data.account, Range: data.dates}
	for day := data.dates.Since; !day.After(data.dates.Until); day = day.AddDate(0, 0, 1) {
		sum, err := ads.SumMetrics(byDay[day.Format(ads.DayLayout)])
		if err != nil {
			return nil, err
		}
		trend.Points = append(trend.Points, TrendPoint{Day: day, Metrics: sum})
	}
	return trend, nil
}
