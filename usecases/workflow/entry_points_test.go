package workflow_usecase

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"vozko/domain/workflow"
)

type recordingEntryPointReader struct {
	triggerCalls [][]string
	sourceCalls  []workflow.EntryPointSourceQuery
	triggers     map[string][]workflow.Node
	sources      *workflow.EntryPointSources
	err          error
}

func (r *recordingEntryPointReader) PublishedTriggers(ids []string) (map[string][]workflow.Node, error) {
	r.triggerCalls = append(r.triggerCalls, ids)
	return r.triggers, r.err
}

func (r *recordingEntryPointReader) EntryPointSources(q workflow.EntryPointSourceQuery) (*workflow.EntryPointSources, error) {
	r.sourceCalls = append(r.sourceCalls, q)
	if r.err != nil {
		return nil, r.err
	}
	return r.sources, nil
}

func entryPointsRepo(t *testing.T) *MockWorkflowRepository {
	t.Helper()
	repo := NewMockWorkflowRepository()
	webhookDraft := workflow.Graph{Nodes: []workflow.Node{{ID: "t", Type: workflow.NodeTypeTriggerWebhook}, {ID: "end", Type: workflow.NodeTypeEnd}}, Edges: []workflow.Edge{{Source: "t", Target: "end"}}}
	for _, w := range []*workflow.Workflow{
		{ID: "wf-1", WorkspaceID: "ws1", Name: "Publicado", Status: workflow.WorkflowStatusActive, Type: workflow.WorkflowTypeMessages, Graph: manualGraph("end"), Version: 1},
		{ID: "wf-2", WorkspaceID: "ws1", Name: "Rascunho", Status: workflow.WorkflowStatusDraft, Type: workflow.WorkflowTypeMessages, Graph: webhookDraft, Version: 1},
	} {
		if err := repo.Create(w); err != nil {
			t.Fatal(err)
		}
	}
	return repo
}

func TestTheListCarriesEntryPointsLoadedOnceForTheWholePage(t *testing.T) {
	repo := entryPointsRepo(t)
	reader := &recordingEntryPointReader{
		triggers: map[string][]workflow.Node{"wf-1": {{ID: "agent", Type: workflow.NodeTypeTriggerAgentCall}}},
		sources:  &workflow.EntryPointSources{Agents: map[string][]workflow.EntryAgent{"wf-1": {{ID: "a-1", Name: "Bia", Active: true}}}},
	}
	list := NewListWorkflowsWithEntryPoints(NewListWorkflowsUseCase(repo), NewEntryPoints(reader))

	page, err := list.Execute(workflow.ListWorkflowsInput{WorkspaceID: "ws1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.triggerCalls) != 1 || !reflect.DeepEqual(reader.triggerCalls[0], []string{"wf-1"}) || len(reader.sourceCalls) != 1 {
		t.Fatalf("trigger reads = %v, source reads = %d, want one of each for the page and published triggers only for published workflows", reader.triggerCalls, len(reader.sourceCalls))
	}
	byID := map[string]*workflow.Workflow{}
	for _, w := range page.Items {
		byID[w.ID] = w
	}
	if eps := byID["wf-1"].EntryPoints; len(eps) != 1 || eps[0].Kind != workflow.EntryAgentCall || eps[0].Readiness != workflow.EntryReady {
		t.Fatalf("published entry points = %+v, want the published agent trigger, ready", eps)
	}
	if eps := byID["wf-2"].EntryPoints; len(eps) != 1 || eps[0].Kind != workflow.EntryWebhook || eps[0].Reason != workflow.EntryBlockedNotPublished {
		t.Fatalf("draft entry points = %+v, want the draft webhook blocked as not published", eps)
	}
}

func TestAnEntryPointReadFailureFailsTheListInsteadOfGuessing(t *testing.T) {
	repo := entryPointsRepo(t)
	broken := errors.New("db down")
	list := NewListWorkflowsWithEntryPoints(NewListWorkflowsUseCase(repo), NewEntryPoints(&recordingEntryPointReader{err: broken}))
	if _, err := list.Execute(workflow.ListWorkflowsInput{WorkspaceID: "ws1"}); !errors.Is(err, broken) {
		t.Fatalf("err = %v, want the read failure", err)
	}
}

func TestAnUnwiredEntryPointReaderIsRefused(t *testing.T) {
	repo := entryPointsRepo(t)
	list := NewListWorkflowsWithEntryPoints(NewListWorkflowsUseCase(repo), NewEntryPoints(nil))
	if _, err := list.Execute(workflow.ListWorkflowsInput{WorkspaceID: "ws1"}); !errors.Is(err, ErrEntryPointsUnavailable) {
		t.Fatalf("err = %v, want unavailable", err)
	}
}

func TestTheDetailCarriesItsEntryPoints(t *testing.T) {
	repo := entryPointsRepo(t)
	reader := &recordingEntryPointReader{sources: &workflow.EntryPointSources{Webhooks: map[string]workflow.WebhookEntry{"wf-2": {Active: true, Method: "POST"}}}}
	uc := NewScopedWorkflowsUseCase(ScopedWorkflowsDeps{
		Get: NewGetWorkflowUseCase(repo), Update: NewUpdateWorkflowUseCase(repo), Versions: NewWorkflowVersionsUseCase(repo), EntryPoints: NewEntryPoints(reader),
	})
	detail, err := uc.Detail(ownerScope("u-1"), "wf-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.EntryPoints) != 1 || detail.EntryPoints[0].Details.Webhook == nil || len(reader.triggerCalls) != 0 {
		t.Fatalf("detail entry points = %+v, published reads = %v", detail.EntryPoints, reader.triggerCalls)
	}
	if reader.sourceCalls[0].WorkspaceID != "ws1" {
		t.Fatalf("query = %+v, want the scope's workspace", reader.sourceCalls[0])
	}
	if _, err := uc.Update(context.Background(), ownerScope("u-1"), "wf-2", &workflow.Workflow{Name: "x", Graph: detail.Graph}, detail.Version); err != nil {
		t.Fatalf("the detail must stay saveable: %v", err)
	}
}
