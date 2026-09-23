package report_renderers

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"vozko/domain/attendance"
	"vozko/domain/report"
)

type AttendanceParams struct {
	DateFrom     string `json:"dateFrom"`
	DateTo       string `json:"dateTo"`
	DepartmentID string `json:"departmentId,omitempty"`
	MemberID     string `json:"memberId,omitempty"`
	CampaignID   string `json:"campaignId,omitempty"`
	CampaignType string `json:"campaignType,omitempty"`
	Channel      string `json:"channel,omitempty"`
	IncludeAI    *bool  `json:"includeAi,omitempty"`
	RankMetric   string `json:"rankMetric,omitempty"`
	TrendBuckets int    `json:"trendBuckets,omitempty"`

	WorkspaceName   string `json:"workspaceName,omitempty"`
	DepartmentLabel string `json:"departmentLabel,omitempty"`
	MemberLabel     string `json:"memberLabel,omitempty"`
	ChannelLabel    string `json:"channelLabel,omitempty"`
}

type AttendanceOverviewSource interface {
	Execute(workspaceID string, filter attendance.OverviewFilter) (*attendance.Overview, error)
}

type AttendanceRenderer struct {
	source AttendanceOverviewSource
	labels LabelResolver
	now    func() time.Time
}

func NewAttendanceRenderer(source AttendanceOverviewSource, labels LabelResolver) *AttendanceRenderer {
	return &AttendanceRenderer{
		source: source,
		labels: labels,
		now:    func() time.Time { return time.Now().UTC() },
	}
}

func (r *AttendanceRenderer) Kind() report.Kind { return report.KindAttendanceOverview }

func (r *AttendanceRenderer) Formats() []report.Format {
	return []report.Format{report.FormatCSV}
}

func (r *AttendanceRenderer) Render(
	ctx context.Context,
	job report.Job,
	progress report.ProgressFunc,
) (report.Artifact, error) {
	if r.source == nil {
		return report.Artifact{}, report.ErrNoRenderer
	}

	var params AttendanceParams
	if err := json.Unmarshal(job.Params, &params); err != nil {
		return report.Artifact{}, fmt.Errorf("attendance report: reading parameters: %w", err)
	}

	filter := attendance.OverviewFilter{
		DepartmentID: params.DepartmentID,
		MemberID:     params.MemberID,
		CampaignID:   params.CampaignID,
		CampaignType: params.CampaignType,
		Channel:      params.Channel,
		RankMetric:   params.RankMetric,
		TrendBuckets: params.TrendBuckets,
		IncludeAI:    params.IncludeAI == nil || *params.IncludeAI,
	}
	if from, ok := parseDayStart(params.DateFrom); ok {
		filter.DateFrom = &from
	}
	if to, ok := parseDayEnd(params.DateTo); ok {
		filter.DateTo = &to
	}

	progress(10)
	if err := ctx.Err(); err != nil {
		return report.Artifact{}, err
	}

	overview, err := r.source.Execute(job.WorkspaceID, filter)
	if err != nil {
		return report.Artifact{}, fmt.Errorf("attendance report: reading the overview: %w", err)
	}
	if overview == nil {
		return report.Artifact{}, report.ErrNotFound
	}

	progress(70)
	t := r.labels.For(job.Locale, "metricsOps.export")
	sections := attendanceSections(t, overview, params, job, r.now())

	progress(95)
	document := report.BuildCSVDocument(sections, true)

	return report.Artifact{
		Data:        []byte(document),
		ContentType: report.FormatCSV.ContentType(),
		Filename:    report.Filename("csv", "atendimento", params.DateFrom, params.DateTo),
		RowCount:    countRows(sections),
	}, nil
}

func attendanceSections(
	t Translate,
	o *attendance.Overview,
	params AttendanceParams,
	job report.Job,
	generatedAt time.Time,
) []report.CSVSection {
	sections := []report.CSVSection{
		{Rows: [][]report.CSVCell{{report.Text(t("title"))}}},
		attendanceFiltersSection(t, params, job, generatedAt),
		attendanceKPISection(t, o),
		attendanceStatusSection(t, o),
	}

	optional := []*report.CSVSection{
		attendancePeriodSection(t, o),
		attendanceProjectionSection(t, o),
		attendanceRevenueSection(t, o),
		attendanceTrendSection(t, o),
		attendanceHourlySection(t, o),
		attendanceDepartmentSection(t, o),
		attendanceMemberSection(t, o),
		attendanceTeamRankingSection(t, o),
		attendanceQualitySection(t, o),
		attendanceReworkSection(t, o),
		attendanceBacklogSection(t, o),
		attendanceReachabilitySection(t, o),
		attendanceChannelSection(t, o),
		attendanceStageSection(t, o),
	}
	for _, section := range optional {
		if section != nil {
			sections = append(sections, *section)
		}
	}

	sections = append(sections, attendanceDetailSections(t, o)...)
	if definitions := attendanceDefinitionsSection(t, o); definitions != nil {
		sections = append(sections, *definitions)
	}
	return sections
}

func attendanceFiltersSection(
	t Translate,
	params AttendanceParams,
	job report.Job,
	generatedAt time.Time,
) report.CSVSection {
	rows := [][]report.CSVCell{}
	if params.WorkspaceName != "" {
		rows = append(rows, []report.CSVCell{report.Text(t("filters.workspace")), report.Text(params.WorkspaceName)})
	}
	rows = append(rows,
		[]report.CSVCell{report.Text(t("filters.generatedAt")), report.Text(generatedAt.UTC().Format(isoMillis))},
		[]report.CSVCell{report.Text(t("filters.dateFrom")), report.Text(params.DateFrom)},
		[]report.CSVCell{report.Text(t("filters.dateTo")), report.Text(params.DateTo)},
		[]report.CSVCell{report.Text(t("filters.department")), report.Text(params.DepartmentLabel)},
		[]report.CSVCell{report.Text(t("filters.member")), report.Text(params.MemberLabel)},
		[]report.CSVCell{report.Text(t("filters.channel")), report.Text(params.ChannelLabel)},
		[]report.CSVCell{report.Text(t("filters.includeAi")), report.Text(yesNo(t, params.IncludeAI == nil || *params.IncludeAI))},
	)
	if params.CampaignID != "" {
		rows = append(rows, []report.CSVCell{report.Text(t("filters.campaign")), report.Text(params.CampaignID)})
		if params.CampaignType != "" {
			rows = append(rows, []report.CSVCell{report.Text(t("filters.campaignType")), report.Text(params.CampaignType)})
		}
	}
	_ = job
	return report.CSVSection{
		Title:  t("sections.filters"),
		Header: []string{t("columns.filter"), t("columns.value")},
		Rows:   rows,
	}
}

func attendanceKPISection(t Translate, o *attendance.Overview) report.CSVSection {
	k := o.KPIs
	rows := [][]report.CSVCell{
		metricRow(t, "kpi.engaged", report.Int(k.Engaged)),
		metricRow(t, "kpi.shellBacklog", report.Int(k.ShellBacklog)),
		metricRow(t, "kpi.totalScoped", report.Int(k.TotalScoped)),
		metricRow(t, "kpi.entriesCreated", report.Int(k.EntriesCreated)),
		metricRow(t, "kpi.finished", report.Int(k.Finished)),
		metricRow(t, "kpi.ongoing", report.Int(k.Ongoing)),
		metricRow(t, "kpi.pending", report.Int(k.Pending)),
		metricRow(t, "kpi.newLeads", report.Int(k.NewLeads)),
		metricRow(t, "kpi.unassignedBacklog", report.Int(k.UnassignedBacklog)),
		metricRow(t, "kpi.avgWaitMins", report.NumberPtr(k.AvgWaitMins)),
		metricRow(t, "kpi.avgHandleMins", report.NumberPtr(k.AvgHandleMins)),
		metricRow(t, "kpi.avgFrtMins", report.NumberPtr(k.AvgFRTMins)),
	}
	if k.CSATAvailable {
		rows = append(rows, metricRow(t, "kpi.avgRating", report.NumberPtr(k.AvgRating)))
	}
	if k.SLAAvailable {
		rows = append(rows,
			metricRow(t, "kpi.frtSlaPercent", report.NumberPtr(k.FRTSLAPercent)),
			metricRow(t, "kpi.resolutionSlaPercent", report.NumberPtr(k.ResolutionSLAPercent)),
		)
	}
	return report.CSVSection{
		Title:  t("sections.kpis"),
		Header: []string{t("columns.metric"), t("columns.value")},
		Rows:   rows,
	}
}

func attendanceStatusSection(t Translate, o *attendance.Overview) report.CSVSection {
	s := o.StatusDistribution
	return report.CSVSection{
		Title:  t("sections.status"),
		Header: []string{t("columns.status"), t("columns.count"), t("columns.percent")},
		Rows: [][]report.CSVCell{
			{report.Text(t("status.finished")), report.Int(s.Finished), pctCell(s.Finished, s.Total)},
			{report.Text(t("status.ongoing")), report.Int(s.Ongoing), pctCell(s.Ongoing, s.Total)},
			{report.Text(t("status.pending")), report.Int(s.Pending), pctCell(s.Pending, s.Total)},
			{report.Text(t("columns.total")), report.Int(s.Total), totalPctCell(s.Total)},
		},
	}
}

func attendancePeriodSection(t Translate, o *attendance.Overview) *report.CSVSection {
	p := o.Period
	rows := [][]report.CSVCell{
		metricRow(t, "executive.available", report.Text(yesNo(t, p.Available))),
		metricRow(t, "executive.reason", report.Text(p.Reason)),
		metricRow(t, "executive.timezone", report.Text(p.Timezone)),
		metricRow(t, "executive.openDaysTotal", report.Int(int64(p.OpenDaysTotal))),
		metricRow(t, "executive.openDaysDone", report.Int(int64(p.OpenDaysDone))),
		metricRow(t, "executive.openDaysLeft", report.Int(int64(p.OpenDaysLeft))),
		metricRow(t, "executive.elapsedPct", report.Number(p.ElapsedPct)),
		metricRow(t, "executive.targetsSet", report.Int(int64(o.Standing.TargetsSet))),
		metricRow(t, "executive.onTrack", report.Int(int64(o.Standing.OnTrack))),
		metricRow(t, "executive.atRisk", report.Int(int64(o.Standing.AtRisk))),
		metricRow(t, "executive.offTrack", report.Int(int64(o.Standing.OffTrack))),
		metricRow(t, "executive.cluster", report.Text(o.Standing.Cluster)),
	}
	return &report.CSVSection{
		Title:  t("sections.period"),
		Header: []string{t("columns.metric"), t("columns.value")},
		Rows:   rows,
	}
}

func attendanceProjectionSection(t Translate, o *attendance.Overview) *report.CSVSection {
	if len(o.Projections) == 0 {
		return nil
	}
	rows := make([][]report.CSVCell, 0, len(o.Projections))
	for _, p := range o.Projections {
		rows = append(rows, []report.CSVCell{
			report.Text(p.MetricKey),
			report.Number(p.Actual),
			report.NumberPtr(p.Target),
			report.NumberPtr(p.Projected),
			report.NumberPtr(p.PerOpenDay),
			report.NumberPtr(p.AttainPct),
			report.Text(string(p.Verdict)),
			report.Text(p.Reason),
		})
	}
	return &report.CSVSection{
		Title: t("sections.projections"),
		Header: []string{
			t("columns.metric"), t("columns.actual"), t("columns.target"),
			t("columns.projected"), t("columns.perOpenDay"), t("columns.attainPct"),
			t("columns.verdict"), t("columns.reason"),
		},
		Rows: rows,
	}
}

func attendanceRevenueSection(t Translate, o *attendance.Overview) *report.CSVSection {
	if !o.Revenue.Available || len(o.Revenue.Currencies) == 0 {
		return nil
	}
	rows := make([][]report.CSVCell, 0, len(o.Revenue.Currencies))
	for _, c := range o.Revenue.Currencies {
		rows = append(rows, []report.CSVCell{
			report.Text(c.Currency),
			report.Int(c.ValueCents),
			report.Int(c.WonCount),
			report.NumberPtr(c.AvgTicket),
			report.NumberPtr(c.PerOpenDay),
			report.IntPtr(c.Projected),
			report.IntPtr(c.PrevClosed),
			report.NumberPtr(c.DeltaPct),
		})
	}
	return &report.CSVSection{
		Title: t("sections.revenue"),
		Header: []string{
			t("columns.currency"), t("columns.valueCents"), t("columns.wonCount"),
			t("columns.avgTicketCents"), t("columns.perOpenDayCents"),
			t("columns.projectedCents"), t("columns.prevClosedCents"), t("columns.deltaPct"),
		},
		Rows: rows,
	}
}

func attendanceTrendSection(t Translate, o *attendance.Overview) *report.CSVSection {
	if len(o.Trend.Series) == 0 {
		return nil
	}
	rows := [][]report.CSVCell{}
	for _, series := range o.Trend.Series {
		for _, point := range series.Points {
			rows = append(rows, []report.CSVCell{
				report.Text(series.MetricKey),
				report.Text(point.Bucket),
				report.Number(point.Value),
				report.Text(yesNo(t, point.Partial)),
				report.Text(yesNo(t, point.Projected)),
				report.Text(series.BestBucket),
				report.NumberPtr(series.BestValue),
			})
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return &report.CSVSection{
		Title: t("sections.trend"),
		Header: []string{
			t("columns.metric"), t("columns.bucket"), t("columns.value"),
			t("columns.partial"), t("columns.projectedFlag"),
			t("columns.bestBucket"), t("columns.bestValue"),
		},
		Rows: rows,
	}
}

func attendanceHourlySection(t Translate, o *attendance.Overview) *report.CSVSection {
	if len(o.Hourly) == 0 {
		return nil
	}
	rows := make([][]report.CSVCell, 0, len(o.Hourly))
	for _, point := range o.Hourly {
		rows = append(rows, []report.CSVCell{
			report.Text(fmt.Sprintf("%02d", point.Hour)),
			report.Int(point.Count),
		})
	}
	return &report.CSVSection{
		Title:  t("sections.hourly"),
		Header: []string{t("columns.hour"), t("columns.conversations")},
		Rows:   rows,
	}
}

func attendanceDepartmentSection(t Translate, o *attendance.Overview) *report.CSVSection {
	if len(o.ByDepartment) == 0 {
		return nil
	}
	rows := make([][]report.CSVCell, 0, len(o.ByDepartment))
	for _, d := range o.ByDepartment {
		name := d.DepartmentName
		if name == "" {
			name = t("noDepartment")
		}
		rows = append(rows, []report.CSVCell{
			report.Text(name),
			report.NumberPtr(d.AvgWaitMins),
			report.NumberPtr(d.AvgHandleMins),
			report.Int(d.Finished),
			report.Int(d.FinishedHuman),
			report.Int(d.FinishedAI),
			report.Int(d.FinishedSystem),
			report.Int(d.Ongoing),
			report.Int(d.Pending),
		})
	}
	return &report.CSVSection{
		Title: t("sections.departments"),
		Header: []string{
			t("columns.department"), t("columns.avgWaitMins"), t("columns.avgHandleMins"),
			t("columns.finished"), t("columns.finishedHuman"), t("columns.finishedAi"),
			t("columns.finishedSystem"), t("columns.ongoing"), t("columns.pending"),
		},
		Rows: rows,
	}
}

func attendanceMemberSection(t Translate, o *attendance.Overview) *report.CSVSection {
	if len(o.ByMember) == 0 {
		return nil
	}
	rows := make([][]report.CSVCell, 0, len(o.ByMember))
	for _, m := range o.ByMember {
		rows = append(rows, []report.CSVCell{
			report.Text(m.DisplayName),
			report.Text(m.Email),
			report.Text(t("actorKind." + m.ActorKind)),
			report.Text(t("presence." + m.Presence)),
			report.NumberPtr(m.AvgResponseMins),
			report.NumberPtr(m.Rating),
			report.Number(m.ResolutionPct),
			report.Int(m.Open),
			report.Int(m.Pending),
			report.Int(m.Resolved),
			report.Int(m.TotalMessages),
			report.NumberPtr(m.AvgMessages),
		})
	}
	return &report.CSVSection{
		Title: t("sections.members"),
		Header: []string{
			t("columns.member"), t("columns.email"), t("columns.kind"), t("columns.presence"),
			t("columns.avgResponseMins"), t("columns.rating"), t("columns.resolutionPct"),
			t("columns.open"), t("columns.pending"), t("columns.resolved"),
			t("columns.totalMessages"), t("columns.avgMessages"),
		},
		Rows: rows,
	}
}

func attendanceTeamRankingSection(t Translate, o *attendance.Overview) *report.CSVSection {
	ranking := o.TeamRanking
	if len(ranking.Members) == 0 && len(ranking.Adjacent) == 0 {
		return nil
	}
	rows := [][]report.CSVCell{}
	appendRanked := func(list []attendance.RankedMember) {
		for _, row := range list {
			rows = append(rows, []report.CSVCell{
				report.Text(row.DisplayName),
				report.Text(t("actorKind." + row.ActorKind)),
				report.Int(row.Resolved),
				report.Number(row.RankMetricValue),
				report.NumberPtr(row.PerOpenDay),
				report.NumberPtr(row.PerOnlineHour),
				report.NumberPtr(row.PctOfTeamAvg),
				report.IntPtr(row.RevenueCents),
				report.NumberPtr(row.AvgTicketCents),
				report.Text(row.Currency),
				report.NumberPtr(row.AvgMessages),
				report.Text(string(row.Class)),
			})
		}
	}
	appendRanked(ranking.Members)
	appendRanked(ranking.Adjacent)
	rows = append(rows, []report.CSVCell{
		report.Text(t("columns.total")),
		report.Empty(),
		report.Int(ranking.Totals.Resolved),
		report.Number(ranking.Totals.RankMetricValue),
		report.NumberPtr(ranking.Totals.PerOpenDay),
		report.NumberPtr(ranking.Totals.PerOnlineHour),
		report.Empty(),
		report.IntPtr(ranking.Totals.RevenueCents),
		report.NumberPtr(ranking.Totals.AvgTicketCents),
		report.Text(ranking.Totals.Currency),
		report.Empty(),
		report.Empty(),
	})
	return &report.CSVSection{
		Title: t("sections.teamRanking"),
		Header: []string{
			t("columns.member"), t("columns.kind"), t("columns.resolved"),
			t("columns.rankMetricValue"), t("columns.perOpenDay"), t("columns.perOnlineHour"),
			t("columns.pctOfTeamAvg"), t("columns.revenueCents"), t("columns.avgTicketCents"),
			t("columns.currency"), t("columns.avgMessages"), t("columns.memberClass"),
		},
		Rows: rows,
	}
}

func attendanceQualitySection(t Translate, o *attendance.Overview) *report.CSVSection {
	if !o.Quality.Available {
		return nil
	}
	rows := [][]report.CSVCell{}
	appendQuality := func(list []attendance.QualityRow) {
		for _, row := range list {
			rows = append(rows, []report.CSVCell{
				report.Text(row.DisplayName),
				report.Text(t("actorKind." + row.ActorKind)),
				report.Int(row.Closes),
				report.Int(row.Captured),
				report.Int(row.Durable),
				report.NumberPtr(row.DurablePct),
				report.Text(string(row.Verdict)),
			})
		}
	}
	appendQuality(o.Quality.Rows)
	appendQuality(o.Quality.Adjacent)
	rows = append(rows, []report.CSVCell{
		report.Text(t("columns.total")),
		report.Empty(),
		report.Int(o.Quality.Team.Closes),
		report.Int(o.Quality.Team.Captured),
		report.Int(o.Quality.Team.Durable),
		report.NumberPtr(o.Quality.Team.DurablePct),
		report.Text(string(o.Quality.Team.Verdict)),
	})
	return &report.CSVSection{
		Title: t("sections.quality"),
		Header: []string{
			t("columns.member"), t("columns.kind"), t("columns.closes"),
			t("columns.captured"), t("columns.durable"), t("columns.durablePct"),
			t("columns.verdict"),
		},
		Rows: rows,
	}
}

func attendanceReworkSection(t Translate, o *attendance.Overview) *report.CSVSection {
	if !o.Rework.Available {
		return nil
	}
	rows := [][]report.CSVCell{}
	appendRework := func(list []attendance.ReworkRow) {
		for _, row := range list {
			if row.Finished == 0 {
				continue
			}
			rows = append(rows, []report.CSVCell{
				report.Text(row.DisplayName),
				report.Text(t("actorKind." + row.ActorKind)),
				report.Int(row.Finished),
				report.Int(row.Reopened),
				report.NumberPtr(row.ReopenRate),
				report.Int(row.Templates),
				costCell(o.Rework.CostAvailable, row.CostMicros),
			})
		}
	}
	appendRework(o.Rework.Rows)
	appendRework(o.Rework.Adjacent)
	appendRework([]attendance.ReworkRow{o.Rework.Unassigned})
	rows = append(rows, []report.CSVCell{
		report.Text(t("columns.total")),
		report.Empty(),
		report.Int(o.Rework.Team.Finished),
		report.Int(o.Rework.Team.Reopened),
		report.NumberPtr(o.Rework.Team.ReopenRate),
		report.Int(o.Rework.Team.Templates),
		costCell(o.Rework.CostAvailable, o.Rework.Team.CostMicros),
	})
	return &report.CSVSection{
		Title: t("sections.rework"),
		Header: []string{
			t("columns.member"), t("columns.kind"), t("columns.closes"),
			t("columns.reopened"), t("columns.reopenRate"),
			t("columns.reworkTemplates"), t("columns.reworkCostMicros"),
		},
		Rows: rows,
	}
}

func attendanceBacklogSection(t Translate, o *attendance.Overview) *report.CSVSection {
	if !o.BacklogXray.Available {
		return nil
	}
	rows := [][]report.CSVCell{}
	for _, dimension := range []attendance.XrayDimension{
		o.BacklogXray.Origin, o.BacklogXray.Assignee, o.BacklogXray.Age,
		o.BacklogXray.Tenure, o.BacklogXray.Returning,
	} {
		for _, bucket := range dimension.Buckets {
			rows = append(rows, []report.CSVCell{
				report.Text(dimension.Dimension),
				report.Text(bucket.Key),
				report.Text(bucket.Label),
				report.Int(bucket.Count),
				report.Number(bucket.Pct),
				report.Int(dimension.Measured),
				report.Int(dimension.Unknown),
			})
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return &report.CSVSection{
		Title: t("sections.backlogXray"),
		Header: []string{
			t("columns.dimension"), t("columns.bucket"), t("columns.label"),
			t("columns.count"), t("columns.percent"),
			t("columns.measured"), t("columns.unknown"),
		},
		Rows: rows,
	}
}

func attendanceReachabilitySection(t Translate, o *attendance.Overview) *report.CSVSection {
	if len(o.BacklogXray.Reachability) == 0 {
		return nil
	}
	rows := make([][]report.CSVCell, 0, len(o.BacklogXray.Reachability))
	for _, row := range o.BacklogXray.Reachability {
		open, closed, pct := report.Empty(), report.Empty(), report.Empty()
		if row.Available {
			open = report.Int(row.WindowOpen)
			closed = report.Int(row.WindowClosed)
			pct = report.Number(row.ClosedPct)
		}
		rows = append(rows, []report.CSVCell{
			report.Text(t("channel." + row.Channel)),
			report.Int(row.Measured),
			open, closed, pct,
			report.Text(row.Reason),
		})
	}
	return &report.CSVSection{
		Title: t("sections.reachability"),
		Header: []string{
			t("columns.channel"), t("columns.measured"), t("columns.windowOpen"),
			t("columns.windowClosed"), t("columns.closedPct"), t("columns.reason"),
		},
		Rows: rows,
	}
}

func attendanceChannelSection(t Translate, o *attendance.Overview) *report.CSVSection {
	if len(o.ChannelMix) == 0 {
		return nil
	}
	rows := make([][]report.CSVCell, 0, len(o.ChannelMix))
	for _, slice := range o.ChannelMix {
		rows = append(rows, []report.CSVCell{
			report.Text(t("channel." + slice.Channel)),
			report.Int(slice.Count),
			report.Number(slice.Pct),
		})
	}
	return &report.CSVSection{
		Title:  t("sections.channels"),
		Header: []string{t("columns.channel"), t("columns.conversations"), t("columns.percent")},
		Rows:   rows,
	}
}

func attendanceStageSection(t Translate, o *attendance.Overview) *report.CSVSection {
	if !o.Stages.Available || len(o.Stages.Funnels) == 0 {
		return nil
	}
	rows := [][]report.CSVCell{}
	for _, funnel := range o.Stages.Funnels {
		name := funnel.FunnelName
		if name == "" {
			name = t("noFunnel")
		}
		for _, stage := range funnel.Stages {
			rows = append(rows, []report.CSVCell{
				report.Text(name),
				report.Text(stage.StageName),
				report.Int(stage.Engaged),
				report.Int(stage.Shell),
				report.Number(stage.PctOfFunnel),
				report.Number(stage.PctOfStaged),
				report.NumberPtr(stage.AvgDaysInStage),
				report.NumberPtr(stage.OldestDaysInStage),
				report.Int(stage.Stuck),
				report.Int(int64(stage.StuckAfterDays)),
			})
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return &report.CSVSection{
		Title: t("sections.stages"),
		Header: []string{
			t("columns.funnel"), t("columns.stage"), t("columns.engaged"), t("columns.shell"),
			t("columns.pctOfFunnel"), t("columns.pctOfStaged"), t("columns.avgDaysInStage"),
			t("columns.oldestDaysInStage"), t("columns.stuck"), t("columns.stuckAfterDays"),
		},
		Rows: rows,
	}
}

func attendanceDetailSections(t Translate, o *attendance.Overview) []report.CSVSection {
	sections := []report.CSVSection{}
	add := func(titleKey string, rows [][]report.CSVCell) {
		if len(rows) == 0 {
			return
		}
		sections = append(sections, report.CSVSection{
			Title:  t(titleKey),
			Header: []string{t("columns.metric"), t("columns.value")},
			Rows:   rows,
		})
	}

	if o.FRT.Available {
		add("sections.frt", [][]report.CSVCell{
			metricRow(t, "frt.avgMins", report.NumberPtr(o.FRT.AvgMins)),
			metricRow(t, "frt.medianMins", report.NumberPtr(o.FRT.MedianMins)),
			metricRow(t, "frt.humanAvgMins", report.NumberPtr(o.FRT.HumanAvgMins)),
			metricRow(t, "frt.aiAvgMins", report.NumberPtr(o.FRT.AIAvgMins)),
			metricRow(t, "frt.sampleCount", report.Int(o.FRT.SampleCount)),
			metricRow(t, "frt.humanSamples", report.Int(o.FRT.HumanSamples)),
			metricRow(t, "frt.aiSamples", report.Int(o.FRT.AISamples)),
		})
	}
	if o.AI.Available {
		add("sections.ai", [][]report.CSVCell{
			metricRow(t, "ai.sessions", report.Int(o.AI.Sessions)),
			metricRow(t, "ai.contained", report.Int(o.AI.Contained)),
			metricRow(t, "ai.handedOff", report.Int(o.AI.HandedOff)),
			metricRow(t, "ai.abandoned", report.Int(o.AI.Abandoned)),
			metricRow(t, "ai.openSessions", report.Int(o.AI.OpenSessions)),
			metricRow(t, "ai.containmentRate", report.Number(o.AI.ContainmentRate)),
			metricRow(t, "ai.handoffRate", report.Number(o.AI.HandoffRate)),
			metricRow(t, "ai.avgAiMessages", report.Number(o.AI.AvgAIMessages)),
		})
	}
	if o.Queue.Available {
		add("sections.queue", [][]report.CSVCell{
			metricRow(t, "queue.enqueued", report.Int(o.Queue.Enqueued)),
			metricRow(t, "queue.connected", report.Int(o.Queue.Connected)),
			metricRow(t, "queue.abandoned", report.Int(o.Queue.Abandoned)),
			metricRow(t, "queue.overflow", report.Int(o.Queue.Overflow)),
			metricRow(t, "queue.queueFull", report.Int(o.Queue.QueueFull)),
			metricRow(t, "queue.cancelled", report.Int(o.Queue.Cancelled)),
			metricRow(t, "queue.avgAsaMins", report.NumberPtr(o.Queue.AvgASAMins)),
			metricRow(t, "queue.abandonRate", report.Number(o.Queue.AbandonRate)),
		})
	}
	if o.Occupancy.Available {
		add("sections.occupancy", [][]report.CSVCell{
			metricRow(t, "occupancy.avgOccupancyPct", report.NumberPtr(o.Occupancy.AvgOccupancyPct)),
			metricRow(t, "occupancy.agentsSampled", report.Int(o.Occupancy.AgentsSampled)),
			metricRow(t, "occupancy.teamOccupancyPct", report.NumberPtr(o.Occupancy.TeamOccupancyPct)),
			metricRow(t, "occupancy.teamIdlePct", report.NumberPtr(o.Occupancy.TeamIdlePct)),
		})
	}
	if o.Messaging.Available {
		add("sections.messaging", [][]report.CSVCell{
			metricRow(t, "messaging.avgPerConversation", report.NumberPtr(o.Messaging.AvgMessagesPerConversation)),
			metricRow(t, "messaging.avgInbound", report.NumberPtr(o.Messaging.AvgInbound)),
			metricRow(t, "messaging.avgOutbound", report.NumberPtr(o.Messaging.AvgOutbound)),
			metricRow(t, "messaging.conversationsWithMessages", report.Int(o.Messaging.ConversationsWithMessages)),
		})
	}
	if o.Reopen.Available {
		add("sections.reopen", [][]report.CSVCell{
			metricRow(t, "reopen.reopenedCount", report.Int(o.Reopen.ReopenedCount)),
			metricRow(t, "reopen.finishedCount", report.Int(o.Reopen.FinishedCount)),
			metricRow(t, "reopen.reopenRate", report.NumberPtr(o.Reopen.ReopenRate)),
		})
	}
	if o.FinishedBySource.Available {
		add("sections.finishedBySource", [][]report.CSVCell{
			metricRow(t, "finishedBySource.human", report.Int(o.FinishedBySource.Human)),
			metricRow(t, "finishedBySource.ai", report.Int(o.FinishedBySource.AI)),
			metricRow(t, "finishedBySource.system", report.Int(o.FinishedBySource.System)),
			metricRow(t, "finishedBySource.total", report.Int(o.FinishedBySource.Total)),
		})
	}
	if o.Stages.Available {
		add("sections.stagesTotals", [][]report.CSVCell{
			metricRow(t, "stages.stagedEngaged", report.Int(o.Stages.StagedEngaged)),
			metricRow(t, "stages.stagedShell", report.Int(o.Stages.StagedShell)),
			metricRow(t, "stages.unstagedEngaged", report.Int(o.Stages.UnstagedEngaged)),
			metricRow(t, "stages.unstagedShell", report.Int(o.Stages.UnstagedShell)),
			metricRow(t, "stages.stuck", report.Int(o.Stages.Stuck)),
		})
	}
	return sections
}

func attendanceDefinitionsSection(t Translate, o *attendance.Overview) *report.CSVSection {
	pairs := definitionPairs(o.Definitions)
	if len(pairs) == 0 {
		return nil
	}
	rows := make([][]report.CSVCell, 0, len(pairs))
	for _, pair := range pairs {
		rows = append(rows, []report.CSVCell{
			report.Text(t("definitions." + pair[0])),
			report.Text(pair[1]),
		})
	}
	return &report.CSVSection{
		Title:  t("sections.definitions"),
		Header: []string{t("columns.metric"), t("columns.definition")},
		Rows:   rows,
	}
}

func definitionPairs(definitions attendance.MetricDefinitions) [][2]string {
	encoded, err := json.Marshal(definitions)
	if err != nil {
		return nil
	}
	var decoded map[string]string
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return nil
	}
	keys := make([]string, 0, len(decoded))
	for key, value := range decoded {
		if value == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([][2]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, [2]string{key, decoded[key]})
	}
	return out
}
