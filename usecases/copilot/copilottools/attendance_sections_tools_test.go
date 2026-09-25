package copilottools

import (
	"context"
	"fmt"
	"testing"

	"vozko/domain/attendance"
	"vozko/domain/copilot"
)

func TestAttendanceMetricsReturnsRequestedPageBlocks(t *testing.T) {
	rating := 4.4
	sections := &fakeSections{summary: &attendance.SummarySection{
		KPIs:   attendance.OverviewKPIs{AvgRating: &rating, CSATAvailable: true},
		Hourly: []attendance.HourlyPoint{{Hour: 9, Count: 5}},
	}}
	cc := ownerOn(screen)
	res := NewAttendanceMetricsTool(testDeps(sections)).Execute(context.Background(), cc, map[string]interface{}{
		"metrics": []interface{}{"finished"}, "details": []interface{}{"service_levels", "hourly"},
	})
	if res.Status != copilot.StatusOK || len(sections.filters) != 1 {
		t.Fatalf("status = %v (%s) queries = %d, want one summary read for metrics and details", res.Status, res.Message, len(sections.filters))
	}
	details := res.Data.(map[string]interface{})["details"].(map[string]interface{})
	if sl := details["service_levels"].(attendance.ServiceLevels); sl.CSATAvg == nil || *sl.CSATAvg != 4.4 {
		t.Fatalf("service levels = %+v", sl)
	}
	hourly := details["hourly"].(copilot.DatasetPreview)
	if _, err := cc.Datasets.Get(hourly.DatasetID); err != nil || hourly.Rows[0][0] != "09h" {
		t.Fatalf("hourly = %+v err %v, want a chartable dataset", hourly, err)
	}

	res = NewAttendanceMetricsTool(testDeps(sections)).Execute(context.Background(), cc, map[string]interface{}{"details": []interface{}{"nps"}})
	if res.Status != copilot.StatusError {
		t.Fatalf("unknown block = %+v", res)
	}
}

func TestAttendanceTeamIncludesDepartments(t *testing.T) {
	sections := &fakeSections{}
	res := NewAttendanceTeamTool(testDeps(sections)).Execute(context.Background(), ownerOn(screen), nil)
	departments := res.Data.(map[string]interface{})["departments"].(map[string]interface{})
	if _, ok := departments["top"].(attendance.DepartmentsDigest); !ok {
		t.Fatalf("departments = %+v", departments)
	}
}

func TestAttendanceStagesKeepsStageRowsInADataset(t *testing.T) {
	funnel := attendance.StageFunnelGroup{FunnelName: "Vendas", Total: 60, Stuck: 2}
	for i := 0; i < 30; i++ {
		funnel.Stages = append(funnel.Stages, attendance.StageRow{StageName: fmt.Sprint("etapa ", i), Total: 2})
	}
	sections := &fakeSections{stages: attendance.OverviewStages{Available: true, Stuck: 2, Funnels: []attendance.StageFunnelGroup{funnel}}}
	cc := ownerOn(screen)
	res := NewAttendanceStagesTool(testDeps(sections)).Execute(context.Background(), cc, nil)
	if res.Status != copilot.StatusOK {
		t.Fatalf("status = %v (%s)", res.Status, res.Message)
	}
	data := res.Data.(map[string]interface{})
	if got := data["stages"].(attendance.StagesDigest); got.Stuck != 2 || got.Funnels[0].Stages != 30 {
		t.Fatalf("stages = %+v", got)
	}
	handle := data["dataset"].(copilot.DatasetPreview)
	if handle.RowCount != 30 || handle.Rows != nil {
		t.Fatalf("handle = %+v, want 30 stage rows kept server-side", handle)
	}
}

func TestAttendanceReworkAndLive(t *testing.T) {
	sections := &fakeSections{
		rework: attendance.OverviewRework{Available: true, Rows: []attendance.ReworkRow{{DisplayName: "Ana", Reopened: 3}}},
		live:   attendance.LiveSection{Live: attendance.OverviewLive{Online: 5, Agents: []attendance.OverviewLiveAgent{{UserID: "u"}}}},
	}
	cc := ownerOn(screen)
	if res := NewAttendanceReworkTool(testDeps(sections)).Execute(context.Background(), cc, nil); res.Status != copilot.StatusOK {
		t.Fatalf("rework = %+v", res)
	}
	res := NewAttendanceLiveTool(testDeps(sections)).Execute(context.Background(), cc, nil)
	live := res.Data.(map[string]interface{})["live"].(attendance.LiveSection)
	if res.Status != copilot.StatusOK || live.Live.Online != 5 || live.Live.Agents != nil {
		t.Fatalf("live = %+v", res)
	}
}

func TestNewSectionToolsRespectDepartmentScope(t *testing.T) {
	for name, tool := range map[string]copilot.Tool{
		"stages": NewAttendanceStagesTool(testDeps(&fakeSections{})),
		"rework": NewAttendanceReworkTool(testDeps(&fakeSections{})),
		"live":   NewAttendanceLiveTool(testDeps(&fakeSections{})),
	} {
		t.Run(name, func(t *testing.T) {
			if res := tool.Execute(context.Background(), memberOf(knownDepartment), map[string]interface{}{"department_id": otherDepartment}); res.Status != copilot.StatusDenied {
				t.Fatalf("status = %v, want denied", res.Status)
			}
			if m := tool.Meta(); m.Mutating || m.Action != "read" {
				t.Fatalf("meta = %+v, want read-only", m)
			}
		})
	}
}
