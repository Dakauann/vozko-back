package workflow_usecase

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"

	"vozko/domain/pipeline"
	"vozko/domain/workflow"
	workspace_department "vozko/domain/workspace/workspace_department"
)

type stubDealFunnels struct {
	pipelines []*pipeline.Pipeline
	err       error
}

func (s stubDealFunnels) DealPipelines(string) ([]*pipeline.Pipeline, error) {
	return s.pipelines, s.err
}

type scopeRecordingResolver struct {
	scopes []workspace_department.CreationScope
}

func (r *scopeRecordingResolver) Resolve(ctx context.Context, _ string) (string, error) {
	scope, _ := workspace_department.GetCreationScope(ctx)
	r.scopes = append(r.scopes, scope)
	return "", nil
}

func defaultFunnels() stubDealFunnels {
	return stubDealFunnels{pipelines: []*pipeline.Pipeline{
		{ID: "pipe-other", ObjectType: pipeline.ObjectOpportunity},
		{ID: "pipe-default", ObjectType: pipeline.ObjectOpportunity, IsDefault: true},
	}}
}

func TestFormAutomationsListTheWorkflowsThatUseAForm(t *testing.T) {
	repo := NewMockWorkflowRepository()
	trigger := formTriggeredWorkflow("wf-trigger", map[string]interface{}{workflow.FormTriggerFormIDsKey: []interface{}{"form-1"}})
	other := formTriggeredWorkflow("wf-other", map[string]interface{}{workflow.FormTriggerFormIDsKey: []interface{}{"form-2"}})
	sender := formWorkflow(completedEdge())
	sender.ID, sender.Status = "wf-node", workflow.WorkflowStatusDraft
	foreign := formTriggeredWorkflow("wf-foreign", nil)
	foreign.WorkspaceID = "ws-2"
	for _, w := range []*workflow.Workflow{trigger, other, sender, foreign} {
		if err := repo.Create(w); err != nil {
			t.Fatal(err)
		}
	}
	automations, err := NewFormAutomations(repo, NewCreateWorkflowUseCase(repo), defaultFunnels())
	if err != nil {
		t.Fatal(err)
	}

	got, err := automations.List(context.Background(), "ws-1", "form-1")
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].WorkflowID < got[j].WorkflowID })
	want := []FormAutomation{
		{WorkflowID: "wf-node", Name: "Cadastro", Active: false, Use: string(workflow.FormUseNode)},
		{WorkflowID: "wf-trigger", Name: "wf-trigger", Active: true, Use: string(workflow.FormUseTrigger)},
	}
	if len(got) != len(want) {
		t.Fatalf("automations = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("automation %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	if _, err := automations.List(context.Background(), "", "form-1"); !errors.Is(err, workflow.ErrWorkspaceIDRequired) {
		t.Fatalf("no workspace = %v", err)
	}
	if _, err := automations.List(context.Background(), "ws-1", " "); !errors.Is(err, ErrFormAutomationFormRequired) {
		t.Fatalf("no form = %v", err)
	}
}

func TestScaffoldCreatesAnInactiveDraftThatLabelsAndOpensADeal(t *testing.T) {
	repo := NewMockWorkflowRepository()
	resolver := &scopeRecordingResolver{}
	automations, err := NewFormAutomations(repo, NewCreateWorkflowUseCase(repo, resolver), defaultFunnels())
	if err != nil {
		t.Fatal(err)
	}

	id, err := automations.Scaffold(context.Background(), "ws-1", "user-1", "form-1", "Cadastro de clientes")
	if err != nil {
		t.Fatal(err)
	}

	w, _ := repo.FindByID(id)
	if w == nil || w.Status != workflow.WorkflowStatusDraft || w.WorkspaceID != "ws-1" || w.Type != workflow.WorkflowTypeMessages || w.TriggerType != workflow.TriggerFormSubmitted {
		t.Fatalf("workflow = %+v", w)
	}
	if use, ok := w.FormUseOf("form-1"); !ok || use != workflow.FormUseTrigger {
		t.Fatalf("the scaffold does not answer form-1: %q %v", use, ok)
	}
	if _, ok := w.FormUseOf("form-2"); ok {
		t.Fatal("the scaffold answers every form")
	}
	if len(resolver.scopes) != 1 || resolver.scopes[0].UserID != "user-1" {
		t.Fatalf("creation scope = %+v", resolver.scopes)
	}

	byType := map[workflow.NodeType]workflow.Node{}
	for _, n := range w.Graph.Nodes {
		byType[n.Type] = n
	}
	label, deal := byType[workflow.NodeTypeActionAssignLabel], byType[workflow.NodeTypeActionManageOpportunity]
	if label.ID == "" || label.Config["action"] != "add" {
		t.Fatalf("label node = %+v", label)
	}
	if deal.ID == "" || deal.Config["pipeline_id"] != "pipe-default" || deal.Config["action"] != "create" || deal.Config["title"] != "Cadastro de clientes" {
		t.Fatalf("deal node = %+v", deal)
	}
	if _, ok := byType[workflow.NodeTypeEnd]; !ok {
		t.Fatal("the scaffold has no end")
	}
	trigger := byType[workflow.NodeTypeTriggerFormSubmitted]
	path := []string{trigger.ID}
	for current := trigger.ID; ; {
		edges := w.Graph.OutgoingEdges(current)
		if len(edges) == 0 {
			break
		}
		current = edges[0].Target
		path = append(path, current)
	}
	wantPath := []string{trigger.ID, label.ID, deal.ID, byType[workflow.NodeTypeEnd].ID}
	if len(path) != len(wantPath) {
		t.Fatalf("path = %v, want %v", path, wantPath)
	}
	for i := range wantPath {
		if path[i] != wantPath[i] {
			t.Fatalf("path = %v, want %v", path, wantPath)
		}
	}
	if err := workflow.ValidateGraph(&w.Graph, w.Type, nil); err != nil {
		t.Fatalf("the scaffold graph is not sound: %v", err)
	}
}

func TestScaffoldRefusesWhatItCannotBuild(t *testing.T) {
	cases := []struct {
		name                         string
		funnels                      stubDealFunnels
		workspace, user, form, title string
		want                         error
	}{
		{"no workspace", defaultFunnels(), "", "user-1", "form-1", "Cadastro", workflow.ErrWorkspaceIDRequired},
		{"no creator", defaultFunnels(), "ws-1", "", "form-1", "Cadastro", ErrFormAutomationCreatorRequired},
		{"no form", defaultFunnels(), "ws-1", "user-1", "", "Cadastro", ErrFormAutomationFormRequired},
		{"no default deal funnel", stubDealFunnels{pipelines: []*pipeline.Pipeline{{ID: "p", ObjectType: pipeline.ObjectOpportunity}}}, "ws-1", "user-1", "form-1", "Cadastro", ErrNoDefaultDealFunnel},
		{"the funnels cannot be read", stubDealFunnels{err: errors.New("db down")}, "ws-1", "user-1", "form-1", "Cadastro", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := NewMockWorkflowRepository()
			automations, err := NewFormAutomations(repo, NewCreateWorkflowUseCase(repo), tc.funnels)
			if err != nil {
				t.Fatal(err)
			}
			_, err = automations.Scaffold(context.Background(), tc.workspace, tc.user, tc.form, tc.title)
			if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if all, _ := repo.FindByWorkspaceID("ws-1"); len(all) != 0 {
				t.Fatalf("a refused scaffold created %d workflows", len(all))
			}
		})
	}
}

func TestFormAutomationsNeedEveryDependency(t *testing.T) {
	repo := NewMockWorkflowRepository()
	create := NewCreateWorkflowUseCase(repo)
	if _, err := NewFormAutomations(nil, create, defaultFunnels()); err == nil {
		t.Error("no workflows accepted")
	}
	if _, err := NewFormAutomations(repo, nil, defaultFunnels()); err == nil {
		t.Error("no create use case accepted")
	}
	if _, err := NewFormAutomations(repo, create, nil); err == nil {
		t.Error("no deal funnels accepted")
	}
}

type joinedWorkspaceWorkflows struct {
	pairs []workflow.PublishedWorkflow
	reads int
}

func (j *joinedWorkspaceWorkflows) PublishedInWorkspace(string) ([]workflow.PublishedWorkflow, error) {
	j.reads++
	return j.pairs, nil
}

func TestFormAutomationsReadTheWorkspaceInOneJoinedRead(t *testing.T) {
	running := formTriggeredWorkflow("wf-running", map[string]interface{}{workflow.FormTriggerFormIDsKey: []interface{}{"form-1"}})
	pruned := formTriggeredWorkflow("wf-pruned", map[string]interface{}{workflow.FormTriggerFormIDsKey: []interface{}{"form-1"}})
	source := &joinedWorkspaceWorkflows{pairs: []workflow.PublishedWorkflow{
		{Draft: running, Published: running},
		{Draft: pruned, Err: fmt.Errorf("%w: workflow wf-pruned version 4", workflow.ErrVersionNotFound)},
	}}
	repo := NewMockWorkflowRepository()
	automations, err := NewFormAutomations(source, NewCreateWorkflowUseCase(repo), defaultFunnels())
	if err != nil {
		t.Fatal(err)
	}

	got, err := automations.List(context.Background(), "ws-1", "form-1")
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].WorkflowID < got[j].WorkflowID })
	want := []FormAutomation{
		{WorkflowID: "wf-pruned", Name: "wf-pruned", Active: false, Use: string(workflow.FormUseTrigger)},
		{WorkflowID: "wf-running", Name: "wf-running", Active: true, Use: string(workflow.FormUseTrigger)},
	}
	if source.reads != 1 || len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("reads = %d automations = %+v, want %+v", source.reads, got, want)
	}
}

func TestAFormAutomationIsActiveOnlyWhenThePublishedVersionUsesTheForm(t *testing.T) {
	repo := NewMockWorkflowRepository()
	runningOnly := formTriggeredWorkflow("wf-running", map[string]interface{}{workflow.FormTriggerFormIDsKey: []interface{}{"form-1"}})
	draftOnly := formTriggeredWorkflow("wf-draft", map[string]interface{}{workflow.FormTriggerFormIDsKey: []interface{}{"form-2"}})
	for _, w := range []*workflow.Workflow{runningOnly, draftOnly} {
		if err := repo.Create(w); err != nil {
			t.Fatal(err)
		}
	}
	runningOnly.Graph.Nodes[0].Config = map[string]interface{}{workflow.FormTriggerFormIDsKey: []interface{}{"form-9"}}
	draftOnly.Graph.Nodes[0].Config = map[string]interface{}{workflow.FormTriggerFormIDsKey: []interface{}{"form-1"}}
	for _, w := range []*workflow.Workflow{runningOnly, draftOnly} {
		stored, _ := repo.FindByID(w.ID)
		stored.Graph = w.Graph
		if err := repo.Update(stored); err != nil {
			t.Fatal(err)
		}
	}
	automations, err := NewFormAutomations(repo, NewCreateWorkflowUseCase(repo), defaultFunnels())
	if err != nil {
		t.Fatal(err)
	}

	got, err := automations.List(context.Background(), "ws-1", "form-1")
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].WorkflowID < got[j].WorkflowID })
	want := []FormAutomation{
		{WorkflowID: "wf-draft", Name: "wf-draft", Active: false, Use: string(workflow.FormUseTrigger)},
		{WorkflowID: "wf-running", Name: "wf-running", Active: true, Use: string(workflow.FormUseTrigger)},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("automations = %+v, want %+v", got, want)
	}
}
