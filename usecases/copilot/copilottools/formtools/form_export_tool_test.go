package formtools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/report"
	"vozko/usecases/copilot/copilottools/tooltest"
	report_renderers "vozko/usecases/report/renderers"
)

func TestExportWhatsAppFormResponsesStartsTheReportJobAsTheHTTPRouteDoes(t *testing.T) {
	forms := formsWith(knownDetail())
	reports := &fakeReports{}
	d := deps(forms)
	d.Reports = reports
	cc := adminContext()
	cc.Locale = "pt-BR"
	tool := NewExportWhatsAppFormResponsesTool(d)
	args := map[string]interface{}{"form_id": knownForm, "format": "xlsx", "from": "2026-10-01", "to": "2026-10-18"}

	if err := tooltest.Validate(t, tool, cc, args); err != nil {
		t.Fatalf("validate: %v", err)
	}
	res := tool.Execute(context.Background(), cc, args)

	if res.Status != copilot.StatusOK {
		t.Fatalf("res = %+v", res)
	}
	in := reports.in
	if in.WorkspaceID != "ws-1" || in.RequestedBy != "u-1" || !in.RequestedByAdmin || in.Kind != report.KindWhatsAppFormResponses ||
		in.Format != report.FormatXLSX || in.Locale != "pt-BR" || !in.FromClient {
		t.Fatalf("create input = %+v", in)
	}
	var params report_renderers.WhatsAppFormResponsesParams
	if err := json.Unmarshal(in.Params, &params); err != nil {
		t.Fatal(err)
	}
	if params.FormID != knownForm || params.From != "2026-10-01" || params.To != "2026-10-18" {
		t.Fatalf("params = %+v", params)
	}
	data := tooltest.DataOf(res)
	if data["report_id"] != "report-1" || !strings.Contains(data["next"].(string), "Relatórios") {
		t.Fatalf("data = %+v", data)
	}
}

func TestExportWhatsAppFormResponsesDefaultsToCSV(t *testing.T) {
	reports := &fakeReports{}
	d := deps(formsWith(knownDetail()))
	d.Reports = reports

	NewExportWhatsAppFormResponsesTool(d).Execute(context.Background(), tooltest.Member(), map[string]interface{}{"form_id": knownForm})

	if reports.in.Format != report.FormatCSV {
		t.Fatalf("format = %q", reports.in.Format)
	}
}

func TestExportWhatsAppFormResponsesChecksTheFormAndPeriodBeforeApproval(t *testing.T) {
	tool := NewExportWhatsAppFormResponsesTool(deps(formsWith(knownDetail())))
	cases := map[string]map[string]interface{}{
		"unknown form":   {"form_id": otherForm},
		"bad date":       {"form_id": knownForm, "from": "semana passada"},
		"reversed":       {"form_id": knownForm, "from": "2026-10-18", "to": "2026-10-01"},
		"unknown format": {"form_id": knownForm, "format": "pdf"},
	}
	for name, args := range cases {
		if err := tooltest.Validate(t, tool, tooltest.Member(), args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestExportWhatsAppFormResponsesCardNamesTheFormPeriodAndFormat(t *testing.T) {
	fields := NewExportWhatsAppFormResponsesTool(deps(formsWith(knownDetail()))).(copilot.Describer).Describe(context.Background(), tooltest.Member(),
		map[string]interface{}{"form_id": knownForm, "from": "2026-10-01"})

	got := tooltest.FieldMap(fields)
	if got["form"] != "Captação" || got["format"] != "CSV" || !strings.Contains(got["period"], "01/10/2026") {
		t.Fatalf("fields = %+v", got)
	}
}

func TestExportWhatsAppFormResponsesExplainsAReportRefusal(t *testing.T) {
	reports := &fakeReports{err: report.ErrNotAllowed}
	d := deps(formsWith(knownDetail()))
	d.Reports = reports

	res := NewExportWhatsAppFormResponsesTool(d).Execute(context.Background(), tooltest.Member(), map[string]interface{}{"form_id": knownForm})

	if res.Status != copilot.StatusDenied || !strings.Contains(res.Message, "exportar") {
		t.Fatalf("res = %+v", res)
	}
}

func TestExportWhatsAppFormResponsesNeedsTheExportCapability(t *testing.T) {
	tool := NewExportWhatsAppFormResponsesTool(deps(&fakeForms{}))
	if m := tool.Meta(); !m.Mutating || m.Resource != "whatsapp_form_responses" || m.Action != "export" {
		t.Fatalf("meta = %+v", m)
	}
	if !requires(tool, "whatsapp_forms:read", "whatsapp_form_responses:read", "whatsapp_form_responses:export") {
		t.Fatalf("requires %v", requirementKeys(tool))
	}
}
