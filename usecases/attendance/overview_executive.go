package attendance_usecase

import (
	"context"
	"errors"
	"time"

	"vozko/domain/attendance"
	at "vozko/domain/attendance_target"
	wh "vozko/domain/working_hours"
	wsc "vozko/domain/workspace_config"
)

type executiveWindow struct {
	schedule        *wh.Schedule
	period          attendance.Period
	from            time.Time
	to              time.Time
	invalidSchedule bool
}

func (uc *getOverviewUseCase) qualityPolicy(config *wsc.WorkspaceConfig, departmentID string) attendance.QualityPolicy {
	if config == nil || config.OutcomeCapture == nil {
		return attendance.QualityPolicy{}
	}
	capture := config.OutcomeCapture
	if !capture.Enabled {
		return attendance.QualityPolicy{}
	}
	if len(capture.DepartmentIDs) > 0 && departmentID != "" {
		scoped := false
		for _, id := range capture.DepartmentIDs {
			if id == departmentID {
				scoped = true
				break
			}
		}
		if !scoped {
			return attendance.QualityPolicy{}
		}
	}
	return attendance.QualityPolicy{
		Enabled:      true,
		EnabledAt:    capture.EnabledAt,
		Threshold:    capture.DurableThreshold,
		DurableCodes: capture.DurableCodes(),
	}
}

func (uc *getOverviewUseCase) computeSummary(
	ctx context.Context,
	workspaceID string,
	filter attendance.OverviewFilter,
) (*attendance.SummarySection, error) {
	now := uc.clock()
	config, err := uc.schedules.Config(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	filter.Quality = uc.qualityPolicy(config, filter.DepartmentID)

	out, err := uc.repo.ReadSummary(ctx, workspaceID, filter)
	if err != nil {
		return nil, err
	}
	out.Filter = filter
	out.GeneratedAt = now

	window, err := uc.resolveWindow(config, workspaceID, filter, now)
	if err != nil {
		return nil, err
	}
	targets, err := uc.overviewTargets(ctx, workspaceID, window)
	if err != nil {
		return nil, err
	}
	if err := uc.fillRevenue(ctx, workspaceID, filter, window, out); err != nil {
		return nil, err
	}
	fillProjections(window, targets, out)
	return out, nil
}

func (uc *getOverviewUseCase) loadWindow(
	ctx context.Context,
	workspaceID string,
	filter attendance.OverviewFilter,
) (executiveWindow, error) {
	config, err := uc.schedules.Config(ctx, workspaceID)
	if err != nil {
		return executiveWindow{}, err
	}
	return uc.resolveWindow(config, workspaceID, filter, uc.clock())
}

func (uc *getOverviewUseCase) resolveWindow(
	config *wsc.WorkspaceConfig,
	workspaceID string,
	filter attendance.OverviewFilter,
	now time.Time,
) (executiveWindow, error) {
	schedule, err := uc.schedules.ResolveFromConfig(config, workspaceID, filter.DepartmentID)
	if err != nil {
		if !errors.Is(err, ErrScheduleInvalid) {
			return executiveWindow{}, err
		}
		from, to := attendance.MonthRange(anchorOf(filter, now), time.UTC)
		return executiveWindow{
			period:          attendance.Period{Reason: ReasonInvalidSchedule},
			from:            from,
			to:              to,
			invalidSchedule: true,
		}, nil
	}
	from, to := periodFor(schedule, filter, now)
	return executiveWindow{
		schedule: schedule,
		from:     from,
		to:       to,
		period:   attendance.BuildPeriod(schedule, from, to, now),
	}, nil
}

func (uc *getOverviewUseCase) overviewTargets(
	ctx context.Context,
	workspaceID string,
	window executiveWindow,
) ([]at.Target, error) {
	if uc.targets == nil || window.invalidSchedule {
		return nil, nil
	}
	return uc.targets.ListForOverview(ctx, workspaceID, window.from, scheduleLocation(window.schedule))
}

func anchorOf(filter attendance.OverviewFilter, now time.Time) time.Time {
	if filter.DateTo != nil {
		return *filter.DateTo
	}
	return now
}

func (uc *getOverviewUseCase) fillRevenue(
	ctx context.Context,
	workspaceID string,
	filter attendance.OverviewFilter,
	window executiveWindow,
	out *attendance.SummarySection,
) error {
	if filter.DepartmentID != "" {
		out.Revenue = attendance.UnavailableRevenue(attendance.ReasonRevenueNotDepartmentScoped)
		return nil
	}
	if uc.repo == nil {
		out.Revenue = attendance.UnavailableRevenue(attendance.ReasonNoRevenueRepository)
		return nil
	}

	tallies, unattributed, err := uc.repo.GetRevenue(ctx, workspaceID, window.from, window.to)
	if err != nil {
		return err
	}

	ownerID := filter.MemberID
	if ownerID != "" {
		tallies = attendance.RevenueForOwner(tallies, ownerID)
		unattributed = 0
	}

	previous := map[string]int64{}
	prevRows, err := uc.repo.GetRevenueByMonth(
		ctx, workspaceID, window.from.AddDate(0, -1, 0), window.from, scheduleLocation(window.schedule), ownerID)
	if err != nil {
		return err
	}
	for _, row := range prevRows {
		previous[row.Currency] += row.ValueCents
	}

	out.Revenue = attendance.BuildRevenue(tallies, unattributed, window.period, previous)
	return nil
}

func fillProjections(window executiveWindow, targets []at.Target, out *attendance.SummarySection) {
	out.Period = window.period
	out.Projections = make([]attendance.MetricProjection, 0, len(attendance.TargetableMetrics()))

	selector := at.Selector{
		DepartmentID: out.Filter.DepartmentID,
		MemberID:     out.Filter.MemberID,
	}

	for _, spec := range attendance.TargetableMetrics() {
		actual, known := attendance.MetricActual(out, spec.Key)
		var target *float64
		if resolution, found := at.Resolve(targets, spec.Key, selector); found {
			value := resolution.Value
			target = &value
		}
		out.Projections = append(out.Projections, attendance.BuildProjection(spec, actual, known, target, window.period))
	}

	out.Standing = attendance.BuildStanding(out.Projections, attendance.DefaultClusterBands())
}

func (uc *getOverviewUseCase) buildTrend(
	ctx context.Context,
	workspaceID string,
	filter attendance.OverviewFilter,
	window executiveWindow,
	summary *attendance.SummarySection,
) (attendance.Trend, error) {
	if uc.repo == nil {
		return attendance.UnavailableTrend(attendance.ReasonTrendUnavailable), nil
	}
	loc := scheduleLocation(window.schedule)
	buckets := attendance.ClampTrendBuckets(filter.TrendBuckets)

	result, err := uc.repo.GetTrend(ctx, workspaceID, filter, buckets, loc)
	if err != nil {
		return attendance.Trend{}, err
	}

	currentBucket := window.from.In(loc).Format(attendance.TrendBucketLayout)
	projected := projectionByKey(summary.Projections)

	series := make([]attendance.TrendSeries, 0, 5)
	series = append(series,
		attendance.BuildTrend(
			mustMetric(attendance.MetricFinished),
			trendPoints(result.Buckets, currentBucket, func(row attendance.TrendBucketRow) float64 { return float64(row.Finished) }),
			projected[attendance.MetricFinished],
		),
		attendance.BuildTrend(
			mustMetric(attendance.MetricEngaged),
			trendPoints(result.Buckets, currentBucket, func(row attendance.TrendBucketRow) float64 { return float64(row.Engaged) }),
			projected[attendance.MetricEngaged],
		),
		attendance.BuildTrend(
			mustMetric(attendance.MetricEntriesCreated),
			trendPoints(result.Buckets, currentBucket, func(row attendance.TrendBucketRow) float64 { return float64(row.Created) }),
			projected[attendance.MetricEntriesCreated],
		),
		attendance.BuildTrend(
			attendance.MetricSpec{
				Key:       trendPendingStockKey,
				Kind:      attendance.MetricKindCount,
				Direction: attendance.DirectionLowerIsBetter,
			},
			trendPoints(result.Buckets, currentBucket, func(row attendance.TrendBucketRow) float64 { return float64(row.Pending) }),
			nil,
		),
	)

	revenueSeries, ok, err := uc.revenueTrend(ctx, workspaceID, window, currentBucket, filter.MemberID, summary.Revenue)
	if err != nil {
		return attendance.Trend{}, err
	}
	if ok {
		series = append(series, revenueSeries)
	}

	return attendance.Trend{
		Series:     series,
		Unbucketed: result.Unbucketed,
		Available:  true,
	}, nil
}

const trendPendingStockKey = "pending_stock"

func (uc *getOverviewUseCase) revenueTrend(
	ctx context.Context,
	workspaceID string,
	window executiveWindow,
	currentBucket string,
	ownerID string,
	revenue attendance.Revenue,
) (attendance.TrendSeries, bool, error) {
	if !revenue.Available || revenue.MixedCurrencies || len(revenue.Currencies) != 1 {
		return attendance.TrendSeries{}, false, nil
	}
	loc := scheduleLocation(window.schedule)
	windowFrom := window.from.AddDate(0, -(attendance.DefaultTrendBuckets - 1), 0)

	rows, err := uc.repo.GetRevenueByMonth(ctx, workspaceID, windowFrom, window.to, loc, ownerID)
	if err != nil {
		return attendance.TrendSeries{}, false, err
	}

	currency := revenue.Currencies[0].Currency
	byBucket := map[string]int64{}
	for _, row := range rows {
		if row.Currency != currency {
			continue
		}
		byBucket[row.Bucket] += row.ValueCents
	}

	points := make([]attendance.TrendPoint, 0, attendance.DefaultTrendBuckets)
	for i := 0; i < attendance.DefaultTrendBuckets; i++ {
		bucket := windowFrom.AddDate(0, i, 0).Format(attendance.TrendBucketLayout)
		points = append(points, attendance.TrendPoint{
			Bucket:  bucket,
			Value:   float64(byBucket[bucket]),
			Partial: bucket == currentBucket,
		})
	}

	var projected *float64
	if revenue.Currencies[0].Projected != nil {
		value := float64(*revenue.Currencies[0].Projected)
		projected = &value
	}
	return attendance.BuildTrend(mustMetric(attendance.MetricRevenueCents), points, projected), true, nil
}

func (uc *getOverviewUseCase) buildTeamRanking(
	workspaceID string,
	filter attendance.OverviewFilter,
	window executiveWindow,
	summary *attendance.SummarySection,
	members []attendance.MemberRow,
) attendance.TeamRanking {
	onlineMS := map[string]int64{}
	if uc.presence != nil {
		rows, err := uc.presence.Occupancy(workspaceID, &window.from, &window.to)
		if err == nil {
			for _, row := range rows {
				onlineMS[row.UserID] = row.OnlineMS
			}
		}
	}

	revenue := map[string]attendance.OwnerRevenue{}
	if summary.Revenue.Available {
		for _, row := range summary.Revenue.ByOwner {
			if row.OwnerID == "" {
				continue
			}
			existing := revenue[row.OwnerID]
			if existing.Currency != "" && existing.Currency != row.Currency {
				delete(revenue, row.OwnerID)
				continue
			}
			revenue[row.OwnerID] = attendance.OwnerRevenue{
				Currency:   row.Currency,
				ValueCents: existing.ValueCents + row.ValueCents,
				WonCount:   existing.WonCount + row.WonCount,
			}
		}
	}

	return attendance.BuildTeamRanking(
		members,
		filter.RankMetric,
		summary.Period,
		onlineMS,
		revenue,
		attendance.DefaultMemberClassBands(),
	)
}

func trendPoints(
	rows []attendance.TrendBucketRow,
	currentBucket string,
	value func(attendance.TrendBucketRow) float64,
) []attendance.TrendPoint {
	out := make([]attendance.TrendPoint, 0, len(rows))
	for _, row := range rows {
		out = append(out, attendance.TrendPoint{
			Bucket:  row.Bucket,
			Value:   value(row),
			Partial: row.Bucket == currentBucket,
		})
	}
	return out
}

func projectionByKey(projections []attendance.MetricProjection) map[string]*float64 {
	out := make(map[string]*float64, len(projections))
	for i := range projections {
		if projections[i].Projected == nil {
			continue
		}
		out[projections[i].MetricKey] = projections[i].Projected
	}
	return out
}

func mustMetric(key string) attendance.MetricSpec {
	spec, found := attendance.Metric(key)
	if !found {
		return attendance.MetricSpec{Key: key, Kind: attendance.MetricKindCount, Direction: attendance.DirectionHigherIsBetter}
	}
	return spec
}
