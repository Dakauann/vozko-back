package copilottools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/media"
	wc "vozko/domain/whatsapp_campaign"
	wd "vozko/domain/workspace/workspace_department"
)

const (
	knownSheet    = "3c4d5e6f-7081-4923-a4b5-c6d7e8f90a1b"
	knownCampaign = "9f8e7d6c-5b4a-4392-8170-6f5e4d3c2b1a"
)

type fakeImportPreview struct {
	req wc.ImportRequest
	err error
}

func (f *fakeImportPreview) Preview(_ context.Context, req wc.ImportRequest) (*wc.ImportPreview, error) {
	f.req = req
	if f.err != nil {
		return nil, f.err
	}
	if req.WorkspaceID != "ws-1" || req.MediaID != knownSheet {
		return nil, media.ErrMediaNotFound
	}
	return &wc.ImportPreview{
		Headers: []string{"numero", "var1", "var2"}, Variables: 2, TotalRows: 3, ValidRows: 2,
		IssueCounts: map[string]int{wc.IssueInvalidNumber: 1}, Issues: []wc.ImportIssue{{Line: 3, Reason: wc.IssueInvalidNumber, Value: "123"}},
		UnitCostMicros: 60_000, CostMicros: 120_000, BalanceMicros: 5_000_000, Affordable: true,
		Rows: []wc.PhoneInput{{Number: "5584994409624", Variables: []string{"Maria", "10"}}, {Number: "5584994409625", Variables: []string{"João", "11"}}},
	}, nil
}

type fakeCampaignCreate struct {
	got   *wc.Campaign
	scope wd.CreationScope
}

func (f *fakeCampaignCreate) Execute(ctx context.Context, c *wc.Campaign) (*wc.Campaign, error) {
	f.got = c
	f.scope, _ = wd.GetCreationScope(ctx)
	return &wc.Campaign{ID: knownCampaign, Name: c.Name, Metrics: &wc.CampaignMetrics{TotalNumbers: 2, NotEligiblePossibleSpam: 1}}, nil
}

type fakeCampaignStart struct {
	workspace   string
	departments *wd.DepartmentFilter
	err         error
}

func (f *fakeCampaignStart) Start(workspaceID string, departments *wd.DepartmentFilter, id string) (*wc.Campaign, error) {
	f.workspace, f.departments = workspaceID, departments
	if f.err != nil {
		return nil, f.err
	}
	return &wc.Campaign{ID: id, Name: "Black Friday"}, nil
}

type fakeCampaignAccess struct{}

func (fakeCampaignAccess) Owned(workspaceID string, _ *wd.DepartmentFilter, id string) (*wc.Campaign, error) {
	if workspaceID != "ws-1" || id != knownCampaign {
		return nil, wc.ErrCampaignNotFound
	}
	return &wc.Campaign{ID: id, Name: "Black Friday", TemplateID: knownTemplate, Metrics: &wc.CampaignMetrics{TotalNumbers: 40, Pending: 40}}, nil
}

type fakeBalance int64

func (b fakeBalance) GetBalance(string) (int64, error) { return int64(b), nil }

type campaignFixture struct {
	deps    CampaignDeps
	preview *fakeImportPreview
	create  *fakeCampaignCreate
	start   *fakeCampaignStart
}

func newCampaignFixture() campaignFixture {
	f := campaignFixture{preview: &fakeImportPreview{}, create: &fakeCampaignCreate{}, start: &fakeCampaignStart{}}
	f.deps = CampaignDeps{
		Preview: f.preview, Create: f.create, Start: f.start, Access: fakeCampaignAccess{},
		Templates: grantedTemplates{}, Phones: &fakeWorkspacePhones{}, Costs: fakeCosts{}, Balance: fakeBalance(2_000_000),
	}
	return f
}

func creating(cc copilot.Context) context.Context {
	return wd.WithCreationScope(context.Background(), wd.CreationScope{UserID: cc.UserID})
}

func createCampaignArgsFor(extra map[string]interface{}) map[string]interface{} {
	args := map[string]interface{}{"media_id": knownSheet, "template_id": knownTemplate, "name": "Black Friday", "business_phone_id": knownPhone}
	for k, v := range extra {
		args[k] = v
	}
	return args
}

func fieldValue(fields []copilot.Field, key string) string {
	for _, f := range fields {
		if f.Key == key {
			return f.Value
		}
	}
	return ""
}

func TestCampaignToolsDeclareTheirPermissions(t *testing.T) {
	deps := newCampaignFixture().deps
	cases := []struct {
		tool     copilot.Tool
		action   string
		mutating bool
	}{
		{NewPreviewCampaignImportTool(deps), "create", false},
		{NewCreateCampaignTool(deps), "create", true},
		{NewStartCampaignTool(deps), "start", true},
	}
	for _, c := range cases {
		m := c.tool.Meta()
		if m.Resource != "whatsapp_campaigns" || string(m.Action) != c.action || m.Mutating != c.mutating {
			t.Fatalf("%s meta = %+v", c.tool.Definition().Name, m)
		}
	}
}

func TestPreviewCampaignImportReportsRowsCostAndIssuesForTheWorkspace(t *testing.T) {
	f := newCampaignFixture()
	res := NewPreviewCampaignImportTool(f.deps).Execute(context.Background(), member(), map[string]interface{}{
		"media_id": knownSheet, "template_id": knownTemplate, "number_column": "fone", "variable_columns": []interface{}{"nome", "cupom"},
	})
	if res.Status != copilot.StatusOK {
		t.Fatalf("status %s: %s", res.Status, res.Message)
	}
	data := res.Data.(map[string]interface{})
	if data["valid_rows"] != 2 || data["estimated_cost"] != "US$ 0.12" || data["balance_suffices"] != true {
		t.Fatalf("data = %+v", data)
	}
	if f.preview.req.WorkspaceID != "ws-1" || f.preview.req.Mapping.Number != "fone" || len(f.preview.req.Mapping.Variables) != 2 {
		t.Fatalf("request = %+v", f.preview.req)
	}
}

func TestPreviewCampaignImportRefusesInventedIDs(t *testing.T) {
	res := NewPreviewCampaignImportTool(newCampaignFixture().deps).Execute(context.Background(), member(), map[string]interface{}{
		"media_id": "planilha.csv", "template_id": knownTemplate,
	})
	if res.Status != copilot.StatusError || !strings.Contains(res.Message, "media_id") {
		t.Fatalf("result = %+v", res)
	}
}

func TestPreviewCampaignImportExplainsAMissingColumn(t *testing.T) {
	f := newCampaignFixture()
	f.preview.err = wc.ErrImportVariableCount
	res := NewPreviewCampaignImportTool(f.deps).Execute(context.Background(), member(), map[string]interface{}{"media_id": knownSheet, "template_id": knownTemplate})
	if res.Status != copilot.StatusError || !strings.Contains(res.Message, "variable_columns") {
		t.Fatalf("result = %+v", res)
	}
}

func TestCreateCampaignUsesOnlyTheValidRowsReadByTheServer(t *testing.T) {
	f := newCampaignFixture()
	cc := member()
	res := NewCreateCampaignTool(f.deps).Execute(creating(cc), cc, createCampaignArgsFor(map[string]interface{}{
		"phone_numbers": []interface{}{"5500000000000"}, "workspace_id": "ws-2",
	}))
	if res.Status != copilot.StatusOK {
		t.Fatalf("status %s: %s", res.Status, res.Message)
	}
	got := f.create.got
	if got.WorkspaceID != "ws-1" || got.Type != wc.CampaignTypeStandard || got.BusinessPhoneID != knownPhone || len(got.PhoneInputs) != 2 {
		t.Fatalf("campaign = %+v", got)
	}
	if f.create.scope.UserID != "u-1" {
		t.Fatalf("creation scope = %+v", f.create.scope)
	}
	if data := res.Data.(map[string]interface{}); data["blocked_by_spam_protection"] != int64(1) {
		t.Fatalf("data = %+v", data)
	}
}

func TestCreateCampaignCarriesTheChosenDepartment(t *testing.T) {
	f := newCampaignFixture()
	cc := member()
	NewCreateCampaignTool(f.deps).Execute(creating(cc), cc, createCampaignArgsFor(map[string]interface{}{"department_id": knownDepartment}))
	if f.create.scope.RequestedDepartmentID != knownDepartment || f.create.scope.UserID != "u-1" {
		t.Fatalf("scope = %+v", f.create.scope)
	}
}

func TestCreateCampaignFailsClosedWithoutACreationScope(t *testing.T) {
	f := newCampaignFixture()
	res := NewCreateCampaignTool(f.deps).Execute(context.Background(), member(), createCampaignArgsFor(nil))
	if res.Status != copilot.StatusDenied || f.create.got != nil {
		t.Fatalf("result = %+v created %+v", res, f.create.got)
	}
}

func TestCreateCampaignRefusesAFileWithNoValidRow(t *testing.T) {
	f := newCampaignFixture()
	f.deps.Preview = emptyPreview{}
	cc := member()
	res := NewCreateCampaignTool(f.deps).Execute(creating(cc), cc, createCampaignArgsFor(nil))
	if res.Status != copilot.StatusError || f.create.got != nil {
		t.Fatalf("result = %+v", res)
	}
}

type emptyPreview struct{}

func (emptyPreview) Preview(context.Context, wc.ImportRequest) (*wc.ImportPreview, error) {
	return &wc.ImportPreview{TotalRows: 4}, nil
}

func TestCreateCampaignApprovalShowsWhatWillBeCreated(t *testing.T) {
	cc := member()
	fields := NewCreateCampaignTool(newCampaignFixture().deps).(copilot.Describer).Describe(creating(cc), cc, createCampaignArgsFor(nil))
	if fieldValue(fields, "phone") != "+55 84 99440-9624" || fieldValue(fields, "template") != "pedido_saiu" ||
		fieldValue(fields, "contacts") != "2 de 3 linhas" || fieldValue(fields, "estimatedCost") != "US$ 0.12" {
		t.Fatalf("fields = %+v", fields)
	}
}

func TestStartCampaignApprovalShowsTheFinalCostAndBalance(t *testing.T) {
	fields := NewStartCampaignTool(newCampaignFixture().deps).(copilot.Describer).Describe(context.Background(), member(), map[string]interface{}{"campaign_id": knownCampaign})
	if fieldValue(fields, "campaign") != "Black Friday" || fieldValue(fields, "contacts") != "40" ||
		fieldValue(fields, "finalCost") != "US$ 0.34" || fieldValue(fields, "balance") != "US$ 2.00" {
		t.Fatalf("fields = %+v", fields)
	}
}

func TestStartCampaignApprovalHidesForeignCampaigns(t *testing.T) {
	cc := member()
	cc.WorkspaceID = "ws-2"
	fields := NewStartCampaignTool(newCampaignFixture().deps).(copilot.Describer).Describe(context.Background(), cc, map[string]interface{}{"campaign_id": knownCampaign})
	if len(fields) != 1 || fieldValue(fields, "finalCost") != "" {
		t.Fatalf("fields = %+v", fields)
	}
}

func TestStartCampaignStartsWithTheUsersScope(t *testing.T) {
	f := newCampaignFixture()
	cc := member()
	res := NewStartCampaignTool(f.deps).Execute(context.Background(), cc, map[string]interface{}{"campaign_id": knownCampaign})
	if res.Status != copilot.StatusOK || f.start.workspace != "ws-1" || f.start.departments != cc.Departments {
		t.Fatalf("result %+v start %+v", res, f.start)
	}
}

func TestStartCampaignExplainsWhyItCannotStart(t *testing.T) {
	cases := map[error]copilot.Status{
		wc.ErrCampaignNoSubscription: copilot.StatusDenied,
		wc.ErrCampaignNotFound:       copilot.StatusError,
		wc.ErrCampaignAllProcessed:   copilot.StatusError,
	}
	for err, want := range cases {
		f := newCampaignFixture()
		f.start.err = err
		res := NewStartCampaignTool(f.deps).Execute(context.Background(), member(), map[string]interface{}{"campaign_id": knownCampaign})
		if res.Status != want || strings.Contains(res.Message, "falha na operação") {
			t.Fatalf("%v: %+v", err, res)
		}
	}
	f := newCampaignFixture()
	f.start.err = errors.New("queue down")
	if res := NewStartCampaignTool(f.deps).Execute(context.Background(), member(), map[string]interface{}{"campaign_id": knownCampaign}); res.Status != copilot.StatusError {
		t.Fatalf("unexpected error result = %+v", res)
	}
}
