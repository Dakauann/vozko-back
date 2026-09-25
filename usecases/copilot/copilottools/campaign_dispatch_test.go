package copilottools

import (
	"context"
	"fmt"
	"testing"

	"vozko/domain/copilot"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type fakeReports struct {
	access  []wc.ReportAccess
	periods []wc.ReportPeriod
	err     error
	rows    []wce.CampaignFunnel
}

func (f *fakeReports) record(a wc.ReportAccess, p wc.ReportPeriod) error {
	f.access = append(f.access, a)
	f.periods = append(f.periods, p)
	return f.err
}

func (f *fakeReports) Summary(_ context.Context, a wc.ReportAccess, p wc.ReportPeriod) (*wc.ReportSummary, error) {
	return &wc.ReportSummary{CampaignID: a.CampaignID, CampaignName: "Oferta", Funnel: wce.Funnel{Base: 100, Sent: 90, Replied: 12}}, f.record(a, p)
}

func (f *fakeReports) Daily(_ context.Context, a wc.ReportAccess, p wc.ReportPeriod) (*wc.ReportDaily, error) {
	return &wc.ReportDaily{Timezone: "America/Fortaleza", Days: []wce.DayCount{{Day: "2026-09-01", Sent: 5}}}, f.record(a, p)
}

func (f *fakeReports) Failures(_ context.Context, a wc.ReportAccess, p wc.ReportPeriod) (*wc.ReportFailures, error) {
	return &wc.ReportFailures{Reasons: []wce.FailureReasonCount{{Code: 131026, Count: 3}}}, f.record(a, p)
}

func (f *fakeReports) Tags(_ context.Context, a wc.ReportAccess, p wc.ReportPeriod) (*wc.ReportTags, error) {
	return &wc.ReportTags{Tags: []wce.TagCount{{Name: "quente", Count: 4}}}, f.record(a, p)
}

func (f *fakeReports) Campaigns(_ context.Context, a wc.ReportAccess, p wc.ReportPeriod) (*wc.ReportCampaigns, error) {
	return &wc.ReportCampaigns{Campaigns: f.rows}, f.record(a, p)
}

func dispatchTool(reports *fakeReports) copilot.Tool {
	return NewCampaignDispatchTool(reports, testDeps(&fakeSections{}))
}

func TestCampaignDispatchNeedsCampaignAccess(t *testing.T) {
	if m := dispatchTool(&fakeReports{}).Meta(); m.Resource != "whatsapp_campaigns" || m.Action != "read" || m.Mutating {
		t.Fatalf("meta = %+v, want whatsapp_campaigns:read, read-only", m)
	}
}

func TestCampaignDispatchReadsTheScreenAndPassesDaysOnly(t *testing.T) {
	reports := &fakeReports{}
	view := screen
	view.CampaignID = "9d1c2b3a-4e5f-4a6b-8c7d-0e1f2a3b4c5d"
	res := dispatchTool(reports).Execute(context.Background(), ownerOn(view), nil)
	if res.Status != copilot.StatusOK {
		t.Fatalf("status = %v (%s)", res.Status, res.Message)
	}
	// The zone is the report's business (workspace schedule), exactly as on the page; the assistant sends days.
	if reports.periods[0] != (wc.ReportPeriod{DateFrom: "2026-09-01", DateTo: "2026-09-07"}) || reports.access[0].CampaignID != view.CampaignID {
		t.Fatalf("period = %+v access = %+v", reports.periods[0], reports.access[0])
	}
	if reports.access[0].AllowDepartment == nil {
		t.Fatal("the department decision must reach the report")
	}
}

func TestCampaignDispatchScopesTheAllCampaignsView(t *testing.T) {
	reports := &fakeReports{}
	dispatchTool(reports).Execute(context.Background(), memberOf(knownDepartment, otherDepartment), nil)
	if got := reports.access[0].DepartmentIDs; len(got) != 2 {
		t.Fatalf("departments = %v, want the member's own departments", got)
	}

	stranded := &fakeReports{}
	res := dispatchTool(stranded).Execute(context.Background(), memberOf(), nil)
	if res.Status == copilot.StatusOK && !stranded.access[0].DepartmentsBlocked {
		t.Fatal("a member in no department must reach the report as blocked, never as the whole workspace")
	}

	denied := &fakeReports{}
	res = dispatchTool(denied).Execute(context.Background(), memberOf(knownDepartment), map[string]interface{}{"department_id": otherDepartment})
	if res.Status != copilot.StatusDenied || len(denied.access) != 0 {
		t.Fatalf("status = %v reads = %d, want another department refused before reading", res.Status, len(denied.access))
	}
}

func TestCampaignDispatchRefusesInventedIdentifiers(t *testing.T) {
	for name, args := range map[string]map[string]interface{}{
		"campaign":   {"campaign_id": "black friday"},
		"department": {"department_id": "69e1116ce4b2ef3195a9e834"},
	} {
		t.Run(name, func(t *testing.T) {
			reports := &fakeReports{}
			if res := dispatchTool(reports).Execute(context.Background(), ownerOn(copilot.View{}), args); res.Status != copilot.StatusError || len(reports.access) != 0 {
				t.Fatalf("status = %v reads = %d", res.Status, len(reports.access))
			}
		})
	}
}

func TestCampaignDispatchKeepsBigListsServerSide(t *testing.T) {
	rows := make([]wce.CampaignFunnel, 0, 40)
	for i := 0; i < 40; i++ {
		rows = append(rows, wce.CampaignFunnel{CampaignID: fmt.Sprint(i), CampaignName: fmt.Sprint("c", i), Base: int64(i)})
	}
	reports := &fakeReports{rows: rows}
	cc := ownerOn(screen)
	res := dispatchTool(reports).Execute(context.Background(), cc, map[string]interface{}{"details": []interface{}{"daily", "campaigns", "failures", "tags"}})
	if res.Status != copilot.StatusOK {
		t.Fatalf("status = %v (%s)", res.Status, res.Message)
	}
	data := res.Data.(map[string]interface{})
	campaigns := data["campaigns"].(map[string]interface{})
	if top := campaigns["top"].([]wce.CampaignFunnel); len(top) != 10 || top[0].Base != 39 {
		t.Fatalf("top = %+v, want the ten largest", top)
	}
	if handle := campaigns["dataset"].(copilot.DatasetPreview); handle.RowCount != 40 || handle.Rows != nil {
		t.Fatalf("handle = %+v", handle)
	}
	if daily := data["daily"].(map[string]interface{}); daily["timezone"] != "America/Fortaleza" {
		t.Fatalf("daily = %+v, want the zone the report counted in", daily)
	}
}
