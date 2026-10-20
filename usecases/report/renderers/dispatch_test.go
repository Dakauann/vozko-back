package report_renderers

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/report"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/domain/workspace"
)

type exportedBlock struct {
	summary   wc.ReportSummary
	daily     wc.ReportDaily
	campaigns [][]wce.CampaignFunnel
	failures  wc.ReportFailures
	tags      []wce.TagCount
	err       error

	access wc.ReportAccess
	period wc.ReportPeriod
}

func (e *exportedBlock) Export(_ context.Context, access wc.ReportAccess, period wc.ReportPeriod, sink wc.DispatchExportSink) error {
	e.access, e.period = access, period
	if e.err != nil {
		return e.err
	}
	if err := sink.Summary(&e.summary); err != nil {
		return err
	}
	if err := sink.Daily(&e.daily); err != nil {
		return err
	}
	for _, page := range e.campaigns {
		if err := sink.Campaigns(page); err != nil {
			return err
		}
	}
	if err := sink.Failures(&e.failures); err != nil {
		return err
	}
	return sink.Tags(e.tags)
}

func septemberBlock() *exportedBlock {
	return &exportedBlock{
		summary: wc.ReportSummary{Funnel: wce.Funnel{Base: 10, Sent: 8, Delivered: 6, Read: 3, Replied: 1, Failed: 2}},
		daily: wc.ReportDaily{From: "2026-09-01", To: "2026-09-02", Timezone: "America/Sao_Paulo", Days: []wce.DayCount{
			{Day: "2026-09-01", Sent: 5, Delivered: 4, Read: 2, Replied: 1},
			{Day: "2026-09-02", Sent: 3, Delivered: 2, Read: 1},
		}},
		campaigns: [][]wce.CampaignFunnel{
			{{CampaignID: "c1", CampaignName: "Lançamento", Base: 6, Sent: 5, Delivered: 4, Read: 2, Replied: 1, Failed: 1}},
			{{CampaignID: "c2", CampaignName: "Retorno", Base: 4, Sent: 3, Delivered: 2, Read: 1, Failed: 1}},
		},
		failures: wc.ReportFailures{Reasons: []wce.FailureReasonCount{{Code: 131026, Count: 2}}},
		tags:     []wce.TagCount{{LabelID: "l1", Name: "interessado", Count: 3}},
	}
}

var dispatchScopes = scopesOf{
	"owner":  {IsOwnerOrAdmin: true},
	"member": {DepartmentIDs: []string{"d1"}, WorkspaceHasDepartments: true},
}

func dispatchJob(t *testing.T, requester string, params WhatsAppDispatchParams) report.Job {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	return report.Job{WorkspaceID: "ws-1", RequestedBy: requester, Format: report.FormatCSV, Locale: "pt", Params: raw}
}

func september() WhatsAppDispatchParams {
	return WhatsAppDispatchParams{DateFrom: "2026-09-01", DateTo: "2026-09-02"}
}

func TestTheDispatchReportNeedsWhatTheBlockNeedsAndIsReadByItsRequester(t *testing.T) {
	policy, err := NewWhatsAppDispatchRenderer(nil, nil).Policy(report.Job{})
	want := []workspace.PermissionEntry{
		{Resource: workspace.ResourceAttendance, Action: workspace.ActionRead},
		{Resource: workspace.ResourceWhatsAppCampaigns, Action: workspace.ActionRead},
	}
	if err != nil || !reflect.DeepEqual(policy.Required, want) || !policy.RequesterOnly {
		t.Fatalf("policy = %+v, %v", policy, err)
	}
}

func TestTheDispatchPeriodIsCheckedAgainstTheExportMaximumAtCreation(t *testing.T) {
	r := NewWhatsAppDispatchRenderer(nil, nil)
	cases := map[string]struct {
		params WhatsAppDispatchParams
		ok     bool
	}{
		"a year, longer than the block shows": {WhatsAppDispatchParams{DateFrom: "2025-10-01", DateTo: "2026-09-30"}, true},
		"longer than an export may reach":     {WhatsAppDispatchParams{DateFrom: "2023-01-01", DateTo: "2026-09-30"}, false},
		"no period at all":                    {WhatsAppDispatchParams{}, false},
		"an end before the start":             {WhatsAppDispatchParams{DateFrom: "2026-09-30", DateTo: "2026-09-01"}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := r.CheckParams(dispatchJob(t, "owner", tc.params))
			if (err == nil) != tc.ok {
				t.Fatalf("CheckParams() = %v, want ok %v", err, tc.ok)
			}
		})
	}
}

func TestTheDispatchFileCarriesTheBlockSectionsInOrder(t *testing.T) {
	block := septemberBlock()
	r := NewWhatsAppDispatchRenderer(block, dispatchScopes)

	artifact, err := r.Render(context.Background(), dispatchJob(t, "owner", september()), func(int) {})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	text := string(artifact.Data)
	if !strings.HasPrefix(text, report.UTF8BOM) {
		t.Fatal("the file must open as UTF-8 in spreadsheets")
	}
	order := []string{
		"Disparo de campanhas",
		"Funil",
		"Contatos;10;100",
		"Por dia",
		"America/Sao_Paulo",
		"2026-09-01;5;4;2;1",
		"Por campanha",
		"Lançamento;6;5;4;80;2;50;1;50;1",
		"Retorno;4;3;2;66,7;1;50;0;0;1",
		"Total;10;8;6;75;3;50;1;33,3;2",
		"Motivos de falha",
		"131026;2;100",
		"Etiquetas",
		"interessado;3;30",
	}
	at := 0
	for _, piece := range order {
		found := strings.Index(text[at:], piece)
		if found < 0 {
			t.Fatalf("missing %q after position %d in:\n%s", piece, at, text)
		}
		at += found + len(piece)
	}
	if artifact.RowCount == 0 || !strings.HasSuffix(artifact.Filename, ".csv") {
		t.Fatalf("artifact = %d rows, %q", artifact.RowCount, artifact.Filename)
	}
}

func TestTheDispatchFileIsScopedByTheRequesterNeverByTheParams(t *testing.T) {
	block := septemberBlock()
	r := NewWhatsAppDispatchRenderer(block, dispatchScopes)

	params := september()
	params.DepartmentID = "d9"
	if _, err := r.Render(context.Background(), dispatchJob(t, "member", params), func(int) {}); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if !block.access.DepartmentsBlocked {
		t.Fatalf("a member asking for another department must read nothing: %+v", block.access)
	}

	params.DepartmentID = ""
	if _, err := r.Render(context.Background(), dispatchJob(t, "member", params), func(int) {}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(block.access.DepartmentIDs, []string{"d1"}) || block.access.DepartmentsBlocked {
		t.Fatalf("a member without a choice reads their own departments: %+v", block.access)
	}
	if block.period != (wc.ReportPeriod{DateFrom: "2026-09-01", DateTo: "2026-09-02"}) {
		t.Fatalf("period = %+v", block.period)
	}

	if _, err := r.Render(context.Background(), dispatchJob(t, "stranger", params), func(int) {}); err == nil {
		t.Fatal("a requester whose scope cannot be read must get no file")
	}
}

func TestADispatchFileWithoutContactsIsAnEmptyResult(t *testing.T) {
	block := septemberBlock()
	block.summary.Funnel = wce.Funnel{}

	_, err := NewWhatsAppDispatchRenderer(block, dispatchScopes).Render(context.Background(), dispatchJob(t, "owner", september()), func(int) {})

	if !errors.Is(err, report.ErrEmptyResult) {
		t.Fatalf("error = %v, want ErrEmptyResult", err)
	}
}

func TestADispatchCampaignTheRequesterCannotSeeIsForbidden(t *testing.T) {
	block := septemberBlock()
	block.err = wc.ErrCampaignAccessDenied

	_, err := NewWhatsAppDispatchRenderer(block, dispatchScopes).Render(context.Background(), dispatchJob(t, "owner", september()), func(int) {})

	if !errors.Is(err, report.ErrNotAllowed) {
		t.Fatalf("error = %v, want ErrNotAllowed", err)
	}
}

func TestADispatchRendererWithoutItsSourcesRefuses(t *testing.T) {
	_, err := NewWhatsAppDispatchRenderer(nil, dispatchScopes).Render(context.Background(), dispatchJob(t, "owner", september()), func(int) {})
	if !errors.Is(err, report.ErrNoRenderer) {
		t.Fatalf("error = %v", err)
	}
}
