package report_renderers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"testing"

	"vozko/domain/export"
	"vozko/domain/opportunity"
	"vozko/domain/report"
	"vozko/domain/shared"
	"vozko/domain/workspace"
	dept "vozko/domain/workspace/workspace_department"
)

type capturingExporter struct {
	filters []export.ExportFilter
	rows    int
}

func (e *capturingExporter) Export(_ context.Context, filter export.ExportFilter, w io.Writer) (int, error) {
	e.filters = append(e.filters, filter)
	_, _ = w.Write([]byte("a\r\n"))
	return e.rows, nil
}

type scopesOf map[string]*dept.DepartmentFilter

func (s scopesOf) For(_ context.Context, _, userID string, _ bool) (*dept.DepartmentFilter, error) {
	filter, ok := s[userID]
	if !ok {
		return nil, errors.New("no scope")
	}
	return filter, nil
}

func entriesJob(t *testing.T, requester string, entryType export.EntryType, departments ...string) report.Job {
	t.Helper()
	params, err := json.Marshal(ConversationEntriesParams{Filter: export.ExportFilter{EntryType: entryType, Scope: export.Scope{DepartmentIDs: departments}}})
	if err != nil {
		t.Fatal(err)
	}
	return report.Job{WorkspaceID: "ws-1", RequestedBy: requester, Format: report.FormatCSV, Params: params}
}

func TestEntriesRequireTheReadPermissionOfTheirChannel(t *testing.T) {
	r := NewConversationEntriesRenderer(&capturingExporter{}, scopesOf{})
	cases := map[export.EntryType]workspace.Resource{
		export.EntryTypeWhatsApp:           workspace.ResourceWhatsAppCampaigns,
		export.EntryTypeInstagram:          workspace.ResourceInstagramAccounts,
		export.EntryTypeTelegram:           workspace.ResourceTelegramAccounts,
		export.EntryTypeFacebook:           workspace.ResourceFacebookPages,
		export.EntryTypeUnofficialWhatsApp: workspace.ResourceUnofficialWhatsAppCampaigns,
	}
	for entryType, resource := range cases {
		policy, err := r.Policy(entriesJob(t, "u", entryType))
		want := []workspace.PermissionEntry{{Resource: resource, Action: workspace.ActionRead}}
		if err != nil || !reflect.DeepEqual(policy.Required, want) || !policy.RequesterOnly {
			t.Errorf("%s policy = %+v, %v", entryType, policy, err)
		}
	}
	if _, err := r.Policy(entriesJob(t, "u", "fax")); !errors.Is(err, report.ErrNoPolicy) {
		t.Fatalf("an unknown channel = %v", err)
	}
}

func TestEntriesAreScopedByTheRequesterAtRender(t *testing.T) {
	exporter := &capturingExporter{rows: 1}
	r := NewConversationEntriesRenderer(exporter, scopesOf{
		"member": {DepartmentIDs: []string{"d1"}, WorkspaceHasDepartments: true},
		"owner":  {IsOwnerOrAdmin: true},
	})

	if _, err := r.Render(context.Background(), entriesJob(t, "member", export.EntryTypeWhatsApp, "d1", "d9"), func(int) {}); err != nil {
		t.Fatal(err)
	}
	if got := exporter.filters[0].Scope.DepartmentIDs; !reflect.DeepEqual(got, []string{"d1"}) {
		t.Fatalf("a member widened its scope through params: %v", got)
	}
	if _, err := r.Render(context.Background(), entriesJob(t, "member", export.EntryTypeWhatsApp), func(int) {}); err != nil {
		t.Fatal(err)
	}
	if got := exporter.filters[1].Scope.DepartmentIDs; !reflect.DeepEqual(got, []string{"d1"}) {
		t.Fatalf("a member without a narrowing reads its own departments: %v", got)
	}
	if _, err := r.Render(context.Background(), entriesJob(t, "member", export.EntryTypeWhatsApp, "d9"), func(int) {}); !errors.Is(err, report.ErrEmptyResult) {
		t.Fatalf("a member asking for another department = %v", err)
	}
	if _, err := r.Render(context.Background(), entriesJob(t, "stranger", export.EntryTypeWhatsApp), func(int) {}); err == nil {
		t.Fatal("an unreadable scope must fail the render")
	}
	if _, err := NewConversationEntriesRenderer(exporter, nil).Render(context.Background(), entriesJob(t, "owner", export.EntryTypeWhatsApp), func(int) {}); !errors.Is(err, report.ErrNoRenderer) {
		t.Fatalf("a renderer without scopes = %v", err)
	}
}

type capturingOpportunities struct {
	departments []string
	restrict    bool
	assignee    string
}

func (c *capturingOpportunities) Export(_, _ string, departmentIDs []string, restrict bool, assignee string, w io.Writer) (int, error) {
	c.departments, c.restrict, c.assignee = departmentIDs, restrict, assignee
	return 0, nil
}

type dealScopes map[string]opportunity.DealScope

func (d dealScopes) Scope(by shared.Person, _ string) (opportunity.DealScope, error) {
	scope, ok := d[by.UserID]
	if !ok {
		return opportunity.DealScope{}, errors.New("not a member")
	}
	return scope, nil
}

func TestOpportunitiesTakeTheRequesterScopeNeverTheParams(t *testing.T) {
	exporter := &capturingOpportunities{}
	r := NewOpportunitiesRenderer(exporter, dealScopes{"seller": {DepartmentIDs: []string{"d1"}, Restrict: true, AssigneeOverride: "seller"}})
	params := json.RawMessage(`{"pipelineId":"p-1","departmentIds":["d9"],"restrict":false,"assigneeOverrideUserId":""}`)

	if _, err := r.Render(context.Background(), report.Job{WorkspaceID: "ws-1", RequestedBy: "seller", Params: params}, func(int) {}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(exporter.departments, []string{"d1"}) || !exporter.restrict || exporter.assignee != "seller" {
		t.Fatalf("the export used %+v", exporter)
	}
	if _, err := r.Render(context.Background(), report.Job{WorkspaceID: "ws-1", RequestedBy: "stranger", Params: params}, func(int) {}); err == nil {
		t.Fatal("an unreadable scope must fail the render")
	}
	policy, _ := r.Policy(report.Job{})
	if !policy.RequesterOnly || !reflect.DeepEqual(policy.Required, []workspace.PermissionEntry{{Resource: workspace.ResourceConversations, Action: workspace.ActionRead}}) {
		t.Fatalf("opportunities policy = %+v", policy)
	}
}

func TestPoliciesOfTheWorkspaceReports(t *testing.T) {
	attendance, _ := NewAttendanceRenderer(nil, ExportLabels()).Policy(report.Job{})
	if !reflect.DeepEqual(attendance.Required, []workspace.PermissionEntry{{Resource: workspace.ResourceAttendance, Action: workspace.ActionRead}}) {
		t.Fatalf("attendance policy = %+v", attendance)
	}
	balance, _ := NewBalanceTransactionsRenderer(nil).Policy(report.Job{})
	if !reflect.DeepEqual(balance.Required, []workspace.PermissionEntry{{Resource: workspace.ResourceBalance, Action: workspace.ActionRead}}) {
		t.Fatalf("balance policy = %+v", balance)
	}
}
