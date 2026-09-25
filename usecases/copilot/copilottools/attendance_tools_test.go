package copilottools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/attendance"
	"vozko/domain/cache"
	"vozko/domain/copilot"
	wd "vozko/domain/workspace/workspace_department"
)

var fixedNow = func() time.Time { return time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC) }

const (
	knownDepartment = "5f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f"
	otherDepartment = "7a1d2e3f-4b5c-4d6e-8f90-1a2b3c4d5e6f"
	knownMember     = "0b8e6a52-1c3d-4e5f-9a7b-8c9d0e1f2a3b"
)

func testDeps(sections *fakeSections) AttendanceDeps {
	return AttendanceDeps{
		Sections:    sections,
		Departments: fakeListDepartments{depts: []wd.Department{{ID: knownDepartment}, {ID: otherDepartment}}},
		Now:         fixedNow,
	}
}

type fakeSections struct {
	filters []attendance.OverviewFilter
	summary *attendance.SummarySection
	trend   attendance.Trend
	team    attendance.TeamRanking
	stages  attendance.OverviewStages
	rework  attendance.OverviewRework
	live    attendance.LiveSection
	err     error
}

func (f *fakeSections) record(filter attendance.OverviewFilter) error {
	f.filters = append(f.filters, filter)
	return f.err
}

func (f *fakeSections) Summary(_ context.Context, _ string, filter attendance.OverviewFilter) (*attendance.SummarySection, error) {
	if err := f.record(filter); err != nil {
		return nil, err
	}
	if f.summary == nil {
		return &attendance.SummarySection{}, nil
	}
	return f.summary, nil
}

func (f *fakeSections) Trend(_ context.Context, _ string, filter attendance.OverviewFilter) (*attendance.TrendSection, error) {
	return &attendance.TrendSection{Trend: f.trend}, f.record(filter)
}

func (f *fakeSections) Stages(_ context.Context, _ string, filter attendance.OverviewFilter) (*attendance.StagesSection, error) {
	return &attendance.StagesSection{Stages: f.stages}, f.record(filter)
}

func (f *fakeSections) Backlog(_ context.Context, _ string, filter attendance.OverviewFilter) (*attendance.BacklogSection, error) {
	return &attendance.BacklogSection{}, f.record(filter)
}

func (f *fakeSections) Team(_ context.Context, _ string, filter attendance.OverviewFilter) (*attendance.TeamSection, error) {
	return &attendance.TeamSection{TeamRanking: f.team}, f.record(filter)
}

func (f *fakeSections) Rework(_ context.Context, _ string, filter attendance.OverviewFilter) (*attendance.ReworkSection, error) {
	return &attendance.ReworkSection{Rework: f.rework}, f.record(filter)
}

func (f *fakeSections) Live(_ context.Context, _ string, filter attendance.OverviewFilter) (*attendance.LiveSection, error) {
	live := f.live
	return &live, f.record(filter)
}

func ownerOn(view copilot.View) copilot.Context {
	return copilot.Context{
		WorkspaceID: "ws-1",
		Departments: &wd.DepartmentFilter{IsOwnerOrAdmin: true},
		View:        view,
		Datasets:    copilot.NewDatasetStore(),
	}
}

func memberOf(departments ...string) copilot.Context {
	cc := ownerOn(copilot.View{})
	cc.Departments = &wd.DepartmentFilter{DepartmentIDs: departments, WorkspaceHasDepartments: true}
	return cc
}

var screen = copilot.View{Surface: copilot.SurfaceAttendance, DateFrom: "2026-09-01", DateTo: "2026-09-07", DepartmentID: knownDepartment, Channel: "whatsapp"}

func day(f attendance.OverviewFilter) string {
	return f.DateFrom.Format(attendance.DayLayout) + ".." + f.DateTo.Format(attendance.DayLayout)
}

func TestAttendanceQueryDefaultsToWhatIsOnScreen(t *testing.T) {
	sections := &fakeSections{}
	res := NewAttendanceMetricsTool(testDeps(sections)).Execute(context.Background(), ownerOn(screen), map[string]interface{}{"metrics": []interface{}{"finished"}})
	if res.Status != copilot.StatusOK {
		t.Fatalf("status = %v (%s)", res.Status, res.Message)
	}
	got := sections.filters[0]
	// "This week" on the attendance page means the week on screen, not a default the user never chose.
	if day(got) != "2026-09-01..2026-09-07" || got.DepartmentID != knownDepartment || got.Channel != "whatsapp" {
		t.Fatalf("filter = %s dept=%q channel=%q", day(got), got.DepartmentID, got.Channel)
	}
}

func TestAttendanceQueryAllWidensPastTheScreen(t *testing.T) {
	sections := &fakeSections{}
	NewAttendanceMetricsTool(testDeps(sections)).Execute(context.Background(), ownerOn(screen), map[string]interface{}{"department_id": "all", "channel": "all"})
	if got := sections.filters[0]; got.DepartmentID != "" || got.Channel != "" {
		t.Fatalf("filter = %+v, want the whole workspace", got)
	}
}

func TestAttendanceQueryNeverWidensADepartmentMember(t *testing.T) {
	sections := &fakeSections{}
	tool := NewAttendanceMetricsTool(testDeps(sections))

	res := tool.Execute(context.Background(), memberOf(knownDepartment), map[string]interface{}{"department_id": "all"})
	if res.Status != copilot.StatusOK || sections.filters[0].DepartmentID != knownDepartment {
		t.Fatalf("status = %v filter = %+v, want the member's only department", res.Status, sections.filters)
	}

	res = tool.Execute(context.Background(), memberOf(knownDepartment), map[string]interface{}{"department_id": otherDepartment})
	if res.Status != copilot.StatusDenied || len(sections.filters) != 1 {
		t.Fatalf("status = %v queries = %d, want denied before any query", res.Status, len(sections.filters))
	}

	res = tool.Execute(context.Background(), memberOf(knownDepartment, otherDepartment), nil)
	if res.Status != copilot.StatusError || !strings.Contains(res.Message, "department_id") || len(sections.filters) != 1 {
		t.Fatalf("status = %v msg = %q, want the model asked to choose", res.Status, res.Message)
	}

	stranded := memberOf()
	res = tool.Execute(context.Background(), stranded, nil)
	if res.Status != copilot.StatusDenied || len(sections.filters) != 1 {
		t.Fatalf("status = %v, want a member in no department denied", res.Status)
	}
}

func TestAttendanceQueryRefusesAWindowItCannotAfford(t *testing.T) {
	sections := &fakeSections{}
	res := NewAttendanceMetricsTool(testDeps(sections)).Execute(context.Background(), ownerOn(copilot.View{}), map[string]interface{}{"date_from": "2020-01-01", "date_to": "2026-09-25"})
	if res.Status != copilot.StatusError || len(sections.filters) != 0 {
		t.Fatalf("status = %v queries = %d, want the span refused before querying", res.Status, len(sections.filters))
	}
}

func TestAttendanceMetricsComparesWithThePreviousPeriod(t *testing.T) {
	sections := &fakeSections{}
	res := NewAttendanceMetricsTool(testDeps(sections)).Execute(context.Background(), ownerOn(screen), map[string]interface{}{
		"metrics": []interface{}{"finished"}, "compare_previous": true,
	})
	if res.Status != copilot.StatusOK || len(sections.filters) != 2 {
		t.Fatalf("status = %v queries = %d", res.Status, len(sections.filters))
	}
	if got := day(sections.filters[1]); got != "2026-08-25..2026-08-31" {
		t.Fatalf("previous window = %s", got)
	}
	if sections.filters[1].DepartmentID != knownDepartment {
		t.Fatal("the previous period must keep the same scope")
	}
	if _, ok := res.Data.(map[string]interface{})["metrics"].([]attendance.MetricComparison); !ok {
		t.Fatalf("metrics = %T, want comparisons", res.Data.(map[string]interface{})["metrics"])
	}
}

func TestAnalyticsFailuresAreActionableAndDoNotLeakInternals(t *testing.T) {
	busy := &fakeSections{err: cache.ErrGateBusy}
	res := NewAttendanceMetricsTool(testDeps(busy)).Execute(context.Background(), ownerOn(screen), nil)
	if res.Status != copilot.StatusError || !strings.Contains(res.Message, "ocupadas") {
		t.Fatalf("busy = %+v", res)
	}
	broken := &fakeSections{err: errors.New(`pq: relation "conversation_messages" does not exist`)}
	res = NewAttendanceMetricsTool(testDeps(broken)).Execute(context.Background(), ownerOn(screen), nil)
	if strings.Contains(res.Message, "conversation_messages") {
		t.Fatalf("message leaks SQL: %q", res.Message)
	}
}

func TestAttendanceTrendStoresTheTableAndCapsMetrics(t *testing.T) {
	spec, _ := attendance.Metric(attendance.MetricFinished)
	sections := &fakeSections{trend: attendance.Trend{Available: true, Series: []attendance.TrendSeries{
		attendance.BuildTrend(spec, []attendance.TrendPoint{{Bucket: "2026-08", Value: 10}, {Bucket: "2026-09", Value: 4, Partial: true}}, nil),
	}}}
	cc := ownerOn(screen)
	tool := NewAttendanceTrendTool(testDeps(sections))

	res := tool.Execute(context.Background(), cc, map[string]interface{}{"metrics": []interface{}{"finished"}, "months": 99.0})
	if res.Status != copilot.StatusOK {
		t.Fatalf("status = %v (%s)", res.Status, res.Message)
	}
	if sections.filters[0].TrendBuckets != attendance.MaxTrendBuckets {
		t.Fatalf("buckets = %d, want clamped to %d", sections.filters[0].TrendBuckets, attendance.MaxTrendBuckets)
	}
	preview := res.Data.(map[string]interface{})["dataset"].(copilot.DatasetPreview)
	stored, err := cc.Datasets.Get(preview.DatasetID)
	if err != nil || len(stored.Rows) != 2 {
		t.Fatalf("stored = %v err %v", stored, err)
	}

	res = tool.Execute(context.Background(), cc, map[string]interface{}{"metrics": []interface{}{"a", "b", "c", "d", "e"}})
	if res.Status != copilot.StatusError || len(sections.filters) != 1 {
		t.Fatalf("status = %v, want five metrics refused before querying", res.Status)
	}
}

func TestAttendanceTeamKeepsTheWholeTeamOutOfTheReply(t *testing.T) {
	ranking := attendance.TeamRanking{RankMetricKey: "resolved", Available: true}
	for i := 0; i < 30; i++ {
		ranking.Members = append(ranking.Members, attendance.RankedMember{
			MemberRow:       attendance.MemberRow{ActorID: string(rune('a' + i)), DisplayName: string(rune('a' + i)), Email: "x@y.z"},
			RankMetricValue: float64(30 - i),
		})
	}
	sections := &fakeSections{team: ranking}
	cc := ownerOn(screen)
	res := NewAttendanceTeamTool(testDeps(sections)).Execute(context.Background(), cc, map[string]interface{}{
		"order": "bottom", "limit": 3.0, "rank_metric": "nonsense", "include_ai": false,
	})
	if res.Status != copilot.StatusOK {
		t.Fatalf("status = %v (%s)", res.Status, res.Message)
	}
	if f := sections.filters[0]; f.RankMetric != attendance.DefaultRankMetric || f.IncludeAI {
		t.Fatalf("filter = %+v", f)
	}
	data := res.Data.(map[string]interface{})
	team := data["team"].(attendance.TeamDigest)
	if len(team.Members) != 3 || team.Members[0].Value != 1 {
		t.Fatalf("team = %+v", team.Members)
	}
	handle := data["dataset"].(copilot.DatasetPreview)
	if handle.Rows != nil || handle.RowCount != 30 {
		t.Fatalf("handle = %+v, want 30 rows kept server-side and none in the reply", handle)
	}
}

func TestAttendanceQueryRefusesInventedIdentifiersBeforeQuerying(t *testing.T) {
	cases := map[string]map[string]interface{}{
		// A model once sent ObjectId-shaped department ids; Postgres rejected them only after the gate was taken.
		"a department the workspace does not have": {"department_id": "69e1116ce4b2ef3195a9e834"},
		"a member id that is not an actor":         {"member_id": "joao"},
		"an automation id with a malformed tail":   {"member_id": "ai:not-a-uuid"},
		"a channel attendance does not measure":    {"channel": "email"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			sections := &fakeSections{}
			res := NewAttendanceMetricsTool(testDeps(sections)).Execute(context.Background(), ownerOn(copilot.View{}), args)
			if res.Status != copilot.StatusError || len(sections.filters) != 0 {
				t.Fatalf("status = %v queries = %d, want refused before querying", res.Status, len(sections.filters))
			}
			if !strings.Contains(res.Message, "list_departments") && !strings.Contains(res.Message, "attendance_team") && !strings.Contains(res.Message, "canal") {
				t.Fatalf("message %q does not say where the valid values come from", res.Message)
			}
		})
	}
}

func TestAttendanceQueryAcceptsRealIdentifiers(t *testing.T) {
	sections := &fakeSections{}
	res := NewAttendanceMetricsTool(testDeps(sections)).Execute(context.Background(), ownerOn(copilot.View{}), map[string]interface{}{
		"department_id": knownDepartment,
		"member_id":     "ai:" + knownMember,
		"channel":       "unofficial_whatsapp",
	})
	if res.Status != copilot.StatusOK || sections.filters[0].DepartmentID != knownDepartment {
		t.Fatalf("status = %v (%s)", res.Status, res.Message)
	}
}

func TestAttendanceQueryFollowsTheCampaignAndAIFiltersOnScreen(t *testing.T) {
	sections := &fakeSections{}
	hide := false
	view := screen
	view.CampaignID = "9d1c2b3a-4e5f-4a6b-8c7d-0e1f2a3b4c5d"
	view.CampaignType = "whatsapp"
	view.IncludeAI = &hide
	NewAttendanceMetricsTool(testDeps(sections)).Execute(context.Background(), ownerOn(view), nil)
	got := sections.filters[0]
	// "This campaign" on a page filtered to a campaign must not silently answer for every campaign.
	if got.CampaignID != view.CampaignID || got.CampaignType != "whatsapp" || got.IncludeAI {
		t.Fatalf("filter = %+v, want the campaign on screen and AI hidden like the page", got)
	}

	NewAttendanceMetricsTool(testDeps(sections)).Execute(context.Background(), ownerOn(view), map[string]interface{}{"campaign_id": "all"})
	if sections.filters[1].CampaignID != "" || sections.filters[1].CampaignType != "" {
		t.Fatalf("filter = %+v, want every campaign", sections.filters[1])
	}

	res := NewAttendanceMetricsTool(testDeps(sections)).Execute(context.Background(), ownerOn(copilot.View{}), map[string]interface{}{"campaign_id": "black friday"})
	if res.Status != copilot.StatusError || len(sections.filters) != 2 {
		t.Fatalf("status = %v, want an invented campaign refused before querying", res.Status)
	}
}
