package copilottools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"vozko/domain/calendar"
	"vozko/domain/copilot"
	"vozko/domain/label"
	"vozko/domain/shared"
	"vozko/domain/stage"
	tmpl "vozko/domain/whatsapp/template"
	"vozko/domain/workflow"
	wd "vozko/domain/workspace/workspace_department"
)

type fakeTemplates struct {
	workspace string
	input     tmpl.ListInput
}

func (f *fakeTemplates) List(workspaceID string, in tmpl.ListInput) (*shared.PaginatedResult[*tmpl.Template], error) {
	f.workspace, f.input = workspaceID, in
	body := "Olá {{1}}, seu pedido {{2}} saiu."
	return &shared.PaginatedResult[*tmpl.Template]{Items: []*tmpl.Template{{
		ID: "t1", Name: "pedido_saiu", Language: "pt_BR", Category: "UTILITY", Status: tmpl.TemplateStatusApproved,
		Components: []tmpl.TemplateComponent{{Type: "BODY", Text: body}},
	}}, TotalItems: 1, TotalPages: 1, Page: 1}, nil
}

func (f *fakeTemplates) Get(string, string) (*tmpl.Template, error) {
	return nil, tmpl.ErrTemplateNotFound
}

type fakeFunnels struct{ workspace string }

func (f *fakeFunnels) Execute(workspaceID string) ([]stage.FunnelStages, error) {
	f.workspace = workspaceID
	return []stage.FunnelStages{{PipelineID: "p1", PipelineName: "Vendas", IsDefault: true, Stages: []*stage.Stage{{ID: "s1", Name: "Novo", IsInitial: true}, {ID: "s2", Name: "Ganho", IsWon: true}}}}, nil
}

type fakeLabels struct{ workspace string }

func (f *fakeLabels) Execute(workspaceID string) ([]*label.Label, error) {
	f.workspace = workspaceID
	return []*label.Label{{ID: "l1", Name: "VIP"}}, nil
}

type fakeEvents struct{ input calendar.ListEventsInput }

func (f *fakeEvents) Execute(in calendar.ListEventsInput) (*shared.PaginatedResult[*calendar.CalendarEvent], error) {
	f.input = in
	at := time.Date(2026, 9, 26, 13, 0, 0, 0, time.UTC)
	return &shared.PaginatedResult[*calendar.CalendarEvent]{Items: []*calendar.CalendarEvent{{ID: "ev1", Title: "Ligar para Maria", StartTime: at, EndTime: at.Add(30 * time.Minute)}}, TotalItems: 1, TotalPages: 1}, nil
}

type fakeWorkflows struct {
	input workflow.ListWorkflowsInput
	calls int
}

func (f *fakeWorkflows) Execute(in workflow.ListWorkflowsInput) (*shared.PaginatedResult[*workflow.Workflow], error) {
	f.calls++
	f.input = in
	return &shared.PaginatedResult[*workflow.Workflow]{Items: []*workflow.Workflow{{ID: "w1", Name: "Boas-vindas", Status: workflow.WorkflowStatus("active")}}, TotalItems: 1, TotalPages: 1}, nil
}

func catalogDeps() (CatalogDeps, *fakeTemplates, *fakeFunnels, *fakeLabels, *fakeEvents, *fakeWorkflows) {
	t, f, l, e, w := &fakeTemplates{}, &fakeFunnels{}, &fakeLabels{}, &fakeEvents{}, &fakeWorkflows{}
	return CatalogDeps{Templates: t, Funnels: f, Labels: l, Events: e, Workflows: w, Now: func() time.Time { return time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC) }}, t, f, l, e, w
}

func TestCatalogToolsCheckTheSamePermissionsAsTheirPages(t *testing.T) {
	deps, _, _, _, _, _ := catalogDeps()
	want := map[string]string{
		"list_templates":       "whatsapp_templates:read",
		"list_pipelines":       "stages:read",
		"list_labels":          "labels:read",
		"list_calendar_events": "calendar:read",
		"list_workflows":       "workflows:read",
	}
	for _, tool := range []copilot.Tool{NewListTemplatesTool(deps), NewListPipelinesTool(deps), NewListLabelsTool(deps), NewListCalendarEventsTool(deps), NewListWorkflowsTool(deps)} {
		m := tool.Meta()
		if got := string(m.Resource) + ":" + string(m.Action); got != want[tool.Definition().Name] || m.Mutating {
			t.Fatalf("%s meta = %+v", tool.Definition().Name, m)
		}
	}
}

func TestListTemplatesExplainsTheVariables(t *testing.T) {
	deps, templates, _, _, _, _ := catalogDeps()
	res := NewListTemplatesTool(deps).Execute(context.Background(), member(), map[string]interface{}{"search": "pedido"})
	if res.Status != copilot.StatusOK || templates.workspace != "ws-1" || templates.input.Search != "pedido" || templates.input.Status != tmpl.TemplateStatusApproved {
		t.Fatalf("status %s, ws %q, input %+v", res.Status, templates.workspace, templates.input)
	}
	b, _ := json.Marshal(res.Data)
	if !strings.Contains(string(b), `"variables":2`) || !strings.Contains(string(b), "pedido_saiu") {
		t.Fatalf("data = %s", b)
	}
}

func TestListPipelinesListsStagesOfTheWorkspace(t *testing.T) {
	deps, _, funnels, _, _, _ := catalogDeps()
	res := NewListPipelinesTool(deps).Execute(context.Background(), member(), nil)
	b, _ := json.Marshal(res.Data)
	if res.Status != copilot.StatusOK || funnels.workspace != "ws-1" || !strings.Contains(string(b), `"stage_id":"s2"`) || !strings.Contains(string(b), `"won":true`) {
		t.Fatalf("status %s data %s", res.Status, b)
	}
}

func TestListLabelsListsTheWorkspaceLabels(t *testing.T) {
	deps, _, _, labels, _, _ := catalogDeps()
	res := NewListLabelsTool(deps).Execute(context.Background(), member(), nil)
	b, _ := json.Marshal(res.Data)
	if res.Status != copilot.StatusOK || labels.workspace != "ws-1" || !strings.Contains(string(b), "VIP") {
		t.Fatalf("status %s data %s", res.Status, b)
	}
}

func TestListCalendarEventsReadsOnlyTheUsersOwnEvents(t *testing.T) {
	deps, _, _, _, events, _ := catalogDeps()
	res := NewListCalendarEventsTool(deps).Execute(context.Background(), member(), map[string]interface{}{"date_from": "2026-09-26", "date_to": "2026-09-26"})
	in := events.input
	if res.Status != copilot.StatusOK || in.UserID != "u-1" || in.WorkspaceID != "ws-1" {
		t.Fatalf("status %s input %+v", res.Status, in)
	}
	if in.From.Format(time.RFC3339) != "2026-09-26T00:00:00Z" || in.To.Format(time.RFC3339) != "2026-09-27T00:00:00Z" {
		t.Fatalf("range %v .. %v", in.From, in.To)
	}
}

func TestListCalendarEventsDefaultsToTheNextWeek(t *testing.T) {
	deps, _, _, _, events, _ := catalogDeps()
	NewListCalendarEventsTool(deps).Execute(context.Background(), member(), nil)
	if events.input.From.Format(time.RFC3339) != "2026-09-25T00:00:00Z" || events.input.To.Format(time.RFC3339) != "2026-10-02T00:00:00Z" {
		t.Fatalf("range %v .. %v", events.input.From, events.input.To)
	}
}

func TestListCalendarEventsRefusesAnInvertedOrHugeRange(t *testing.T) {
	deps, _, _, _, events, _ := catalogDeps()
	for _, args := range []map[string]interface{}{
		{"date_from": "2026-09-30", "date_to": "2026-09-01"},
		{"date_from": "2026-01-01", "date_to": "2026-12-31"},
	} {
		if res := NewListCalendarEventsTool(deps).Execute(context.Background(), member(), args); res.Status != copilot.StatusError {
			t.Fatalf("args %v: status %s", args, res.Status)
		}
	}
	if events.input.UserID != "" {
		t.Fatal("listed with a bad range")
	}
}

func TestListWorkflowsStaysInsideTheMembersDepartments(t *testing.T) {
	deps, _, _, _, _, workflows := catalogDeps()
	res := NewListWorkflowsTool(deps).Execute(context.Background(), member(), nil)
	if res.Status != copilot.StatusOK || len(workflows.input.DepartmentIDs) != 1 || workflows.input.DepartmentIDs[0] != knownDepartment {
		t.Fatalf("status %s input %+v", res.Status, workflows.input)
	}
}

func TestListWorkflowsIsEmptyForAMemberWithoutDepartment(t *testing.T) {
	deps, _, _, _, _, workflows := catalogDeps()
	cc := member()
	cc.Departments = &wd.DepartmentFilter{WorkspaceHasDepartments: true}
	res := NewListWorkflowsTool(deps).Execute(context.Background(), cc, nil)
	b, _ := json.Marshal(res.Data)
	if res.Status != copilot.StatusOK || workflows.calls != 0 || !strings.Contains(string(b), `"total":0`) {
		t.Fatalf("status %s after %d lists: %s", res.Status, workflows.calls, b)
	}
}

func (f *fakeTemplates) Create(string, string, tmpl.CreateTemplateInput) (*tmpl.CreateTemplateOutput, error) {
	return nil, nil
}
