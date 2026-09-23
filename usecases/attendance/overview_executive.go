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

type executiveInputs struct {
	schedule *wh.Schedule
	period   attendance.Period
	from     time.Time
	to       time.Time
	targets  []at.Target
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

func (uc *getOverviewUseCase) prepare(
	ctx context.Context,
	workspaceID string,
	filter *attendance.OverviewFilter,
	now time.Time,
) (*wsc.WorkspaceConfig, error) {
	config, err := uc.schedules.Config(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	filter.Quality = uc.qualityPolicy(config, filter.DepartmentID)
	return config, nil
}

func (uc *getOverviewUseCase) resolveInputs(
	ctx context.Context,
	workspaceID string,
	filter attendance.OverviewFilter,
	config *wsc.WorkspaceConfig,
	now time.Time,
) (executiveInputs, error) {
	out := executiveInputs{}

	schedule, err := uc.schedules.ResolveFromConfig(config, workspaceID, filter.DepartmentID)
	if err != nil {
		if !errors.Is(err, ErrScheduleInvalid) {
			return out, err
		}
		out.period = attendance.Period{Reason: ReasonInvalidSchedule}
		out.from, out.to = attendance.MonthRange(anchorOf(filter, now), time.UTC)
		return out, nil
	}
	out.schedule = schedule
	out.from, out.to = periodFor(schedule, filter, now)
	out.period = attendance.BuildPeriod(schedule, out.from, out.to, now)

	if uc.targets == nil {
		return out, nil
	}
	targets, err := uc.targets.ListForOverview(ctx, workspaceID, out.from, scheduleLocation(schedule))
	if err != nil {
		return out, err
	}
	out.targets = targets
	return out, nil
}

func anchorOf(filter attendance.OverviewFilter, now time.Time) time.Time {
	if filter.DateTo != nil {
		return *filter.DateTo
	}
	return now
}

func (uc *getOverviewUseCase) fillRevenue(
	workspaceID string,
	filter attendance.OverviewFilter,
	inputs executiveInputs,
	out *attendance.Overview,
) error {
	if filter.DepartmentID != "" {
		out.Revenue = attendance.UnavailableRevenue(attendance.ReasonRevenueNotDepartmentScoped)
		return nil
	}
	if uc.repo == nil {
		out.Revenue = attendance.UnavailableRevenue(attendance.ReasonNoRevenueRepository)
		return nil
	}

	tallies, unattributed, err := uc.repo.GetRevenue(workspaceID, inputs.from, inputs.to)
	if err != nil {
		return err
	}

	ownerID := filter.MemberID
	if ownerID != "" {
		tallies = attendance.RevenueForOwner(tallies, ownerID)
		unattributed = 0
	}

	previous := map[string]int64{}
	prevFrom := inputs.from.AddDate(0, -1, 0)
	prevRows, err := uc.repo.GetRevenueByMonth(
		workspaceID, prevFrom, inputs.from, scheduleLocation(inputs.schedule), ownerID)
	if err != nil {
		return err
	}
	for _, row := range prevRows {
		previous[row.Currency] += row.ValueCents
	}

	out.Revenue = attendance.BuildRevenue(tallies, unattributed, inputs.period, previous)
	return nil
}

func (uc *getOverviewUseCase) fillProjections(inputs executiveInputs, out *attendance.Overview) {
	out.Period = inputs.period
	out.Projections = make([]attendance.MetricProjection, 0, len(attendance.TargetableMetrics()))

	selector := at.Selector{
		DepartmentID: out.Filter.DepartmentID,
		MemberID:     out.Filter.MemberID,
	}

	for _, spec := range attendance.TargetableMetrics() {
		actual, known := attendance.MetricActual(out, spec.Key)
		var target *float64
		if resolution, found := at.Resolve(inputs.targets, spec.Key, selector); found {
			value := resolution.Value
			target = &value
		}
		out.Projections = append(out.Projections, attendance.BuildProjection(spec, actual, known, target, inputs.period))
	}

	out.Standing = attendance.BuildStanding(out.Projections, attendance.DefaultClusterBands())
}

func (uc *getOverviewUseCase) fillTrend(
	workspaceID string,
	filter attendance.OverviewFilter,
	inputs executiveInputs,
	out *attendance.Overview,
) error {
	if uc.repo == nil {
		out.Trend = attendance.UnavailableTrend(attendance.ReasonTrendUnavailable)
		return nil
	}
	loc := scheduleLocation(inputs.schedule)
	buckets := attendance.ClampTrendBuckets(filter.TrendBuckets)

	result, err := uc.repo.GetTrend(workspaceID, filter, buckets, loc)
	if err != nil {
		return err
	}

	currentBucket := inputs.from.In(loc).Format(attendance.TrendBucketLayout)
	projected := projectionByKey(out.Projections)

	series := make([]attendance.TrendSeries, 0, 4)
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

	if revenueSeries, ok := uc.revenueTrend(workspaceID, inputs, currentBucket, filter.MemberID, out); ok {
		series = append(series, revenueSeries)
	}

	out.Trend = attendance.Trend{
		Series:     series,
		Unbucketed: result.Unbucketed,
		Available:  true,
	}
	return nil
}

const trendPendingStockKey = "pending_stock"

func (uc *getOverviewUseCase) revenueTrend(
	workspaceID string,
	inputs executiveInputs,
	currentBucket string,
	ownerID string,
	out *attendance.Overview,
) (attendance.TrendSeries, bool) {
	if uc.repo == nil || !out.Revenue.Available || out.Revenue.MixedCurrencies || len(out.Revenue.Currencies) != 1 {
		return attendance.TrendSeries{}, false
	}
	loc := scheduleLocation(inputs.schedule)
	windowFrom := inputs.from.AddDate(0, -(attendance.DefaultTrendBuckets - 1), 0)

	rows, err := uc.repo.GetRevenueByMonth(workspaceID, windowFrom, inputs.to, loc, ownerID)
	if err != nil {
		return attendance.TrendSeries{}, false
	}

	currency := out.Revenue.Currencies[0].Currency
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
	if out.Revenue.Currencies[0].Projected != nil {
		value := float64(*out.Revenue.Currencies[0].Projected)
		projected = &value
	}
	return attendance.BuildTrend(mustMetric(attendance.MetricRevenueCents), points, projected), true
}

func (uc *getOverviewUseCase) fillTeamRanking(
	workspaceID string,
	filter attendance.OverviewFilter,
	inputs executiveInputs,
	out *attendance.Overview,
) {
	onlineMS := map[string]int64{}
	if uc.presence != nil {
		rows, err := uc.presence.Occupancy(workspaceID, &inputs.from, &inputs.to)
		if err == nil {
			for _, row := range rows {
				onlineMS[row.UserID] = row.OnlineMS
			}
		}
	}

	revenue := map[string]attendance.OwnerRevenue{}
	if out.Revenue.Available {
		for _, row := range out.Revenue.ByOwner {
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

	out.TeamRanking = attendance.BuildTeamRanking(
		out.ByMember,
		filter.RankMetric,
		inputs.period,
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
