package report_renderers

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"vozko/domain/report"
	businessphone "vozko/domain/whatsapp/business_phone"
	wf "vozko/domain/whatsappform"
	"vozko/domain/workspace"
)

const (
	formWorkspace = "ws-forms"
	formRequester = "u-requester"
	exportFormID  = "6f1c2b3a-4d5e-4f60-8a7b-9c0d1e2f3a4b"
	formVersionV1 = "7a1c2b3a-4d5e-4f60-8a7b-9c0d1e2f3a01"
	formVersionV2 = "7a1c2b3a-4d5e-4f60-8a7b-9c0d1e2f3a02"
)

type fakeForms struct{ forms map[string]*wf.Form }

func (f fakeForms) Get(_ context.Context, workspaceID, formID string) (*wf.Form, error) {
	form, ok := f.forms[formID]
	if !ok || form.WorkspaceID != workspaceID {
		return nil, wf.ErrFormNotFound
	}
	return form, nil
}

type fakeVersions struct{ versions []wf.Version }

func (f fakeVersions) List(_ context.Context, workspaceID, formID string) ([]wf.Version, error) {
	var out []wf.Version
	for _, v := range f.versions {
		if v.WorkspaceID == workspaceID && v.FormID == formID {
			out = append(out, v)
		}
	}
	return out, nil
}

type fakeResponsePages struct {
	pages   []wf.Page[wf.Response]
	queries []wf.ResponseQuery
}

func (f *fakeResponsePages) List(_ context.Context, q wf.ResponseQuery) (wf.Page[wf.Response], error) {
	f.queries = append(f.queries, q)
	if len(f.queries) > len(f.pages) {
		return wf.Page[wf.Response]{}, nil
	}
	return f.pages[len(f.queries)-1], nil
}

type fakeFormScopes map[string]wf.ReadScope

func (f fakeFormScopes) ReadScope(userID, _ string, _ bool) (wf.ReadScope, bool) {
	scope, ok := f[userID]
	return scope, ok
}

type fakeLeadNames map[string]string

func (f fakeLeadNames) Names(_ context.Context, _ string, ids []string) (map[string]string, error) {
	out := map[string]string{}
	for _, id := range ids {
		if name, ok := f[id]; ok {
			out[id] = name
		}
	}
	return out, nil
}

type fakeFormPhones map[string]string

func (f fakeFormPhones) FindByID(id string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	number, ok := f[id]
	if !ok {
		return nil, businessphone.ErrPhoneNumberNotFound
	}
	return &businessphone.WhatsAppBusinessPhoneNumber{ID: id, DisplayPhoneNumber: number}, nil
}

type fixedLocation struct{ name string }

func (f fixedLocation) Location(context.Context, string, string) (*time.Location, error) {
	return time.LoadLocation(f.name)
}

type heldPermissions map[string]bool

func (h heldPermissions) HasWorkspacePermission(userID, _, resource, action string, _ bool) bool {
	return h[userID+"|"+resource+":"+action]
}

func sensitiveHolder(userID string) heldPermissions {
	held := heldPermissions{}
	entries, _ := workspace.CapabilitiesRequire([]workspace.CapabilityKey{wf.CapabilityResponsesSensitive})
	for _, e := range entries {
		held[userID+"|"+string(e.Resource)+":"+string(e.Action)] = true
	}
	return held
}

func formField(key string, kind wf.FieldKind, label string) wf.Block {
	return wf.Block{Key: "b_" + key, Field: &wf.Field{Key: key, Kind: kind, Label: label}}
}

func formVersions() []wf.Version {
	colorV1 := formField("f_color", wf.FieldRadio, "Cor antiga")
	colorV1.Field.Options = []wf.Option{{ID: "o_red", Title: "Vermelho"}}
	v1 := wf.Version{ID: formVersionV1, WorkspaceID: formWorkspace, FormID: exportFormID, Number: 1, Document: wf.Document{Screens: []wf.Screen{{Key: "s1", Blocks: []wf.Block{
		formField("f_name", wf.FieldShortText, "Nome"),
		formField("f_old", wf.FieldShortText, "Antigo"),
		colorV1,
	}}}}}
	name := formField("f_name", wf.FieldShortText, "Nome")
	name.Field.Question = "Qual o seu nome?"
	color := formField("f_color", wf.FieldCheckbox, "Cores")
	color.Field.Options = []wf.Option{{ID: "o_red", Title: "Vermelho"}, {ID: "o_blue", Title: "Azul"}}
	cpf := formField("f_cpf", wf.FieldShortText, "CPF")
	cpf.Field.Sensitive = true
	v2 := wf.Version{ID: formVersionV2, WorkspaceID: formWorkspace, FormID: exportFormID, Number: 2, Document: wf.Document{Screens: []wf.Screen{
		{Key: "s1", Blocks: []wf.Block{name, color}},
		{Key: "s2", Blocks: []wf.Block{formField("f_period", wf.FieldDateRange, "Período"), formField("f_doc", wf.FieldDocument, "Documento"), cpf}},
	}}}
	return []wf.Version{v2, v1}
}

func formResponses() []wf.Page[wf.Response] {
	received := time.Date(2026, time.October, 10, 15, 0, 0, 0, time.UTC)
	first := wf.Response{
		ID: "r-1", WorkspaceID: formWorkspace, FormID: exportFormID, VersionID: formVersionV2, Origin: wf.OriginCampaign, Match: wf.MatchByToken,
		LeadID: "l-1", CampaignID: "c-1", BusinessPhoneID: "p-1", ContactNumber: "5511988887777", SubmittedAt: received,
		Answers: wf.Answers{Values: map[string]wf.Answer{
			"f_name":   {Key: "f_name", Kind: wf.FieldShortText, Text: "=Ana"},
			"f_color":  {Key: "f_color", Kind: wf.FieldCheckbox, OptionIDs: []string{"o_red", "o_blue"}},
			"f_period": {Key: "f_period", Kind: wf.FieldDateRange, Range: &wf.DateRange{Start: "2026-10-01", End: "2026-10-05"}},
			"f_doc":    {Key: "f_doc", Kind: wf.FieldDocument, Files: []wf.File{{ID: "m-1", Name: "rg.pdf"}, {ID: "m-2", Name: "cpf.jpg"}}},
			"f_cpf":    {Key: "f_cpf", Kind: wf.FieldShortText, Text: "12345678900"},
		}},
	}
	second := wf.Response{
		ID: "r-2", WorkspaceID: formWorkspace, FormID: exportFormID, VersionID: formVersionV1, Origin: wf.OriginInbox, Match: wf.MatchByContext,
		BusinessPhoneID: "p-gone", ContactNumber: "5511900001111", SubmittedAt: received.Add(-time.Hour),
		Answers: wf.Answers{Values: map[string]wf.Answer{
			"f_name":  {Key: "f_name", Kind: wf.FieldShortText, Text: "Caio"},
			"f_old":   {Key: "f_old", Kind: wf.FieldShortText, Text: "x"},
			"f_color": {Key: "f_color", Kind: wf.FieldRadio, OptionIDs: []string{"o_red"}},
		}},
	}
	third := wf.Response{
		ID: "r-3", WorkspaceID: formWorkspace, Match: wf.UnmatchedNoToken, BusinessPhoneID: "p-1", ContactNumber: "5511922223333",
		SubmittedAt: received.Add(-2 * time.Hour),
		Answers:     wf.Answers{Values: map[string]wf.Answer{}, Unknown: map[string]json.RawMessage{"name": json.RawMessage(`"Bia"`), "age": json.RawMessage(`3`)}},
	}
	return []wf.Page[wf.Response]{{Items: []wf.Response{first, second}, Next: "cursor-1"}, {Items: []wf.Response{third}}}
}

type formExportFixture struct {
	responses *fakeResponsePages
	deps      WhatsAppFormResponsesDeps
}

func newFormExportFixture(permissions heldPermissions) *formExportFixture {
	responses := &fakeResponsePages{pages: formResponses()}
	return &formExportFixture{responses: responses, deps: WhatsAppFormResponsesDeps{
		Forms:       fakeForms{forms: map[string]*wf.Form{exportFormID: {ID: exportFormID, WorkspaceID: formWorkspace, Name: "Cadastro"}}},
		Versions:    fakeVersions{versions: formVersions()},
		Responses:   responses,
		Scopes:      fakeFormScopes{formRequester: {Restrict: true, DepartmentIDs: []string{"d-1"}, AssigneeUserID: formRequester}},
		LeadNames:   fakeLeadNames{"l-1": "Ana Souza"},
		Phones:      fakeFormPhones{"p-1": "+55 11 4000-0000"},
		Locations:   fixedLocation{name: "America/Sao_Paulo"},
		Permissions: permissions,
	}}
}

func formExportJob(t *testing.T, format report.Format, params WhatsAppFormResponsesParams) report.Job {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	return report.Job{WorkspaceID: formWorkspace, RequestedBy: formRequester, Kind: report.KindWhatsAppFormResponses, Format: format, Locale: "pt", Params: raw}
}

func readFormCSV(t *testing.T, data []byte) [][]string {
	t.Helper()
	if !bytes.HasPrefix(data, []byte(report.UTF8BOM)) {
		t.Fatal("the csv opens with a BOM so spreadsheets read it as UTF-8")
	}
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), report.UTF8BOM)))
	reader.Comma = ';'
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return records
}

func renderFormExport(t *testing.T, fx *formExportFixture, format report.Format, params WhatsAppFormResponsesParams) report.Artifact {
	t.Helper()
	artifact, err := NewWhatsAppFormResponsesRenderer(fx.deps).Render(context.Background(), formExportJob(t, format, params), func(int) {})
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

var wantFormHeader = []string{
	"Recebida em", "Contato", "Número", "Lead", "Origem", "Campanha", "Versão",
	"Qual o seu nome?", "Cores", "Período (início)", "Período (fim)", "Documento", "CPF", "Antigo",
	"Correspondência", "Outras respostas",
}

func TestFormResponsesExportMergesTheColumnsOfEveryVersionAndMasksSensitiveAnswers(t *testing.T) {
	fx := newFormExportFixture(heldPermissions{})
	artifact := renderFormExport(t, fx, report.FormatCSV, WhatsAppFormResponsesParams{FormID: exportFormID})
	records := readFormCSV(t, artifact.Data)
	if !reflect.DeepEqual(records[0], wantFormHeader) {
		t.Fatalf("header = %q", records[0])
	}
	want := [][]string{
		{"2026-10-10 12:00:00", "5511988887777", "'+55 11 4000-0000", "Ana Souza", "Campanha", "c-1", "2",
			"'=Ana", "Vermelho, Azul", "01/10/2026", "05/10/2026", "••••", "••••", "", "Pelo token do envio", ""},
		{"2026-10-10 11:00:00", "5511900001111", "", "", "Conversa", "n/d", "1",
			"Caio", "Vermelho", "", "", "", "", "x", "Pela mensagem respondida", ""},
		{"2026-10-10 10:00:00", "5511922223333", "'+55 11 4000-0000", "", "", "n/d", "",
			"", "", "", "", "", "", "", "Sem correspondência: sem token", "age: 3 | name: Bia"},
	}
	if !reflect.DeepEqual(records[1:], want) {
		t.Fatalf("rows =\n%q\nwant\n%q", records[1:], want)
	}
	if artifact.RowCount != 3 || artifact.ContentType != report.FormatCSV.ContentType() || !strings.HasSuffix(artifact.Filename, ".csv") {
		t.Fatalf("artifact = %d %q %q", artifact.RowCount, artifact.ContentType, artifact.Filename)
	}
}

func TestFormResponsesExportRevealsSensitiveAnswersToTheirHolders(t *testing.T) {
	fx := newFormExportFixture(sensitiveHolder(formRequester))
	records := readFormCSV(t, renderFormExport(t, fx, report.FormatCSV, WhatsAppFormResponsesParams{FormID: exportFormID}).Data)
	if records[1][11] != "rg.pdf, cpf.jpg" || records[1][12] != "12345678900" {
		t.Fatalf("a holder of the sensitive capability reads the answers, got %q", records[1])
	}
}

func TestFormResponsesExportPagesWithTheRequestersResolvedScope(t *testing.T) {
	fx := newFormExportFixture(heldPermissions{})
	renderFormExport(t, fx, report.FormatCSV, WhatsAppFormResponsesParams{
		FormID: exportFormID, From: "2026-10-01", To: "2026-10-10", Origins: []string{"campaign"}, PhoneIDs: []string{"p-1"},
		CampaignID: "c-1", Match: "matched",
	})
	if len(fx.responses.queries) != 2 {
		t.Fatalf("pages read = %d", len(fx.responses.queries))
	}
	first, second := fx.responses.queries[0], fx.responses.queries[1]
	wantScope := wf.ReadScope{Resolved: true, Restrict: true, DepartmentIDs: []string{"d-1"}, AssigneeUserID: formRequester}
	if !reflect.DeepEqual(first.Scope, wantScope) || first.WorkspaceID != formWorkspace || first.FormID != exportFormID {
		t.Fatalf("query = %+v", first)
	}
	if first.After != "" || second.After != "cursor-1" || first.Limit != wf.MaxPageSize {
		t.Fatalf("paging = %q %q %d", first.After, second.After, first.Limit)
	}
	from, to := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, time.October, 11, 0, 0, 0, 0, time.UTC).Add(-time.Nanosecond)
	if first.From == nil || !first.From.Equal(from) || first.To == nil || !first.To.Equal(to) {
		t.Fatalf("period = %v %v", first.From, first.To)
	}
	if !reflect.DeepEqual(first.Origins, []wf.SendOrigin{wf.OriginCampaign}) || !reflect.DeepEqual(first.PhoneIDs, []string{"p-1"}) ||
		first.CampaignID != "c-1" || first.Match != wf.MatchFilterMatched {
		t.Fatalf("filters = %+v", first)
	}
}

func TestFormResponsesExportOfChosenVersionsOnlyHasTheirFields(t *testing.T) {
	fx := newFormExportFixture(heldPermissions{})
	fx.responses.pages = []wf.Page[wf.Response]{{Items: formResponses()[0].Items[1:]}}
	records := readFormCSV(t, renderFormExport(t, fx, report.FormatCSV, WhatsAppFormResponsesParams{FormID: exportFormID, VersionIDs: []string{formVersionV1}}).Data)
	want := []string{"Recebida em", "Contato", "Número", "Lead", "Origem", "Campanha", "Versão", "Nome", "Antigo", "Cor antiga", "Correspondência", "Outras respostas"}
	if !reflect.DeepEqual(records[0], want) {
		t.Fatalf("header = %q", records[0])
	}
	if !reflect.DeepEqual(fx.responses.queries[0].VersionIDs, []string{formVersionV1}) {
		t.Fatalf("versions = %v", fx.responses.queries[0].VersionIDs)
	}
}

func TestFormResponsesExportAsXLSXOpensWithTheHeaderRow(t *testing.T) {
	fx := newFormExportFixture(heldPermissions{})
	artifact := renderFormExport(t, fx, report.FormatXLSX, WhatsAppFormResponsesParams{FormID: exportFormID})
	if artifact.ContentType != report.FormatXLSX.ContentType() || !strings.HasSuffix(artifact.Filename, ".xlsx") || artifact.RowCount != 3 {
		t.Fatalf("artifact = %q %q %d", artifact.ContentType, artifact.Filename, artifact.RowCount)
	}
	file, err := excelize.OpenReader(bytes.NewReader(artifact.Data))
	if err != nil {
		t.Fatalf("the workbook does not open: %v", err)
	}
	defer file.Close()
	rows, err := file.GetRows(file.GetSheetList()[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 || !reflect.DeepEqual(rows[0], wantFormHeader) {
		t.Fatalf("rows = %q", rows)
	}
	if rows[1][7] != "'=Ana" || rows[1][12] != "••••" {
		t.Fatalf("first row = %q", rows[1])
	}
}

func TestFormResponsesExportFailsClosedWithoutAScope(t *testing.T) {
	fx := newFormExportFixture(heldPermissions{})
	fx.deps.Scopes = fakeFormScopes{}
	_, err := NewWhatsAppFormResponsesRenderer(fx.deps).Render(context.Background(), formExportJob(t, report.FormatCSV, WhatsAppFormResponsesParams{FormID: exportFormID}), func(int) {})
	if !errors.Is(err, report.ErrNotAllowed) {
		t.Fatalf("err = %v", err)
	}
	if len(fx.responses.queries) != 0 {
		t.Fatal("no response is read without a resolved scope")
	}
}

func TestFormResponsesExportRefusesWhatItCannotRender(t *testing.T) {
	fx := newFormExportFixture(heldPermissions{})
	fx.responses.pages = nil
	if _, err := NewWhatsAppFormResponsesRenderer(fx.deps).Render(context.Background(), formExportJob(t, report.FormatCSV, WhatsAppFormResponsesParams{FormID: exportFormID}), func(int) {}); !errors.Is(err, report.ErrEmptyResult) {
		t.Fatalf("no responses = %v", err)
	}
	missing := newFormExportFixture(heldPermissions{})
	missing.deps.Versions = fakeVersions{versions: formVersions()[:1]}
	if _, err := NewWhatsAppFormResponsesRenderer(missing.deps).Render(context.Background(), formExportJob(t, report.FormatCSV, WhatsAppFormResponsesParams{FormID: exportFormID}), func(int) {}); err == nil {
		t.Fatal("a response whose version cannot be read must fail the render")
	}
	if _, err := NewWhatsAppFormResponsesRenderer(WhatsAppFormResponsesDeps{}).Render(context.Background(), formExportJob(t, report.FormatCSV, WhatsAppFormResponsesParams{FormID: exportFormID}), func(int) {}); !errors.Is(err, report.ErrNoRenderer) {
		t.Fatalf("a renderer without dependencies = %v", err)
	}
}

func TestFormResponsesExportPolicyNeedsTheExportCapabilityAndStaysWithTheRequester(t *testing.T) {
	r := NewWhatsAppFormResponsesRenderer(WhatsAppFormResponsesDeps{})
	policy, err := r.Policy(formExportJob(t, report.FormatCSV, WhatsAppFormResponsesParams{FormID: exportFormID}))
	if err != nil {
		t.Fatal(err)
	}
	for _, need := range []workspace.PermissionEntry{
		{Resource: workspace.ResourceWhatsAppFormResponses, Action: workspace.ActionRead},
		{Resource: workspace.ResourceWhatsAppFormResponses, Action: workspace.ActionExport},
	} {
		found := false
		for _, required := range policy.Required {
			found = found || required == need
		}
		if !found {
			t.Fatalf("policy %+v misses %s", policy.Required, need.Key())
		}
	}
	if !policy.RequesterOnly {
		t.Fatal("a scoped and possibly unmasked file is readable by its requester only")
	}
	if r.Kind() != report.KindWhatsAppFormResponses || !reflect.DeepEqual(r.Formats(), []report.Format{report.FormatCSV, report.FormatXLSX}) {
		t.Fatalf("kind %q formats %v", r.Kind(), r.Formats())
	}
	if report.IsInternal(r) {
		t.Fatal("the kind is offered through /reports")
	}
}

func TestFormResponsesExportParams(t *testing.T) {
	r := NewWhatsAppFormResponsesRenderer(WhatsAppFormResponsesDeps{})
	valid := []WhatsAppFormResponsesParams{
		{FormID: exportFormID},
		{FormID: exportFormID, From: "2026-10-01", To: "2026-10-31T23:59:59-03:00", Origins: []string{"inbox", "campaign"}, Match: "unmatched"},
		{FormID: exportFormID, VersionIDs: []string{formVersionV1}, PhoneIDs: []string{"p-1"}, Match: "matched"},
	}
	for _, params := range valid {
		if err := report.CheckParams(r, formExportJob(t, report.FormatCSV, params)); err != nil {
			t.Errorf("%+v = %v", params, err)
		}
	}
	invalid := map[string]WhatsAppFormResponsesParams{
		"no form":           {},
		"a form not a uuid": {FormID: "form-1"},
		"an unreadable day": {FormID: exportFormID, From: "10/01/2026"},
		"a reversed period": {FormID: exportFormID, From: "2026-10-31", To: "2026-10-01"},
		"an unknown origin": {FormID: exportFormID, Origins: []string{"fax"}},
		"an unknown match":  {FormID: exportFormID, Match: "maybe"},
	}
	for name, params := range invalid {
		if err := report.CheckParams(r, formExportJob(t, report.FormatCSV, params)); !errors.Is(err, report.ErrInvalidParams) {
			t.Errorf("%s = %v", name, err)
		}
	}
	broken := report.Job{WorkspaceID: formWorkspace, Params: json.RawMessage(`{"formId":`)}
	if err := report.CheckParams(r, broken); !errors.Is(err, report.ErrInvalidParams) {
		t.Errorf("unreadable params = %v", err)
	}
}
