package workflow_usecase

import (
	"context"
	"errors"
	"testing"

	label_domain "vozko/domain/label"
	stage_domain "vozko/domain/stage"
	"vozko/domain/workflow"
	"vozko/usecases/workflow/node_executors"
)

type missingLabels struct {
	label_domain.Repository
}

func (missingLabels) FindByID(string) (*label_domain.Label, error) {
	return nil, nil
}

type unreachableFunnels struct{}

func (unreachableFunnels) PipelineStages(string, string) ([]*stage_domain.Stage, error) {
	return nil, errors.New("database unreachable")
}

func gateCatalog() []workflow.NodeDefinition {
	shared := []workflow.NodeScope{workflow.NodeScopeShared}
	return append(workflow.BuiltinDefinitions(),
		node_executors.NewWaitDurationExecutor().Definition(),
		workflow.NodeDefinition{Type: workflow.NodeTypeActionAssignLabel, Category: workflow.NodeCategoryAction, Scopes: shared, ConfigSchema: []workflow.ConfigField{{Key: "label_id", Required: true}}},
		workflow.NodeDefinition{Type: workflow.NodeTypeActionManageOpportunity, Category: workflow.NodeCategoryAction, Scopes: shared, ConfigSchema: []workflow.ConfigField{{Key: "pipeline_id"}}},
	)
}

func gate(repo *MockWorkflowRepository) *activateWorkflowUseCase {
	uc := NewActivateWorkflowUseCase(repo).(*activateWorkflowUseCase)
	uc.SetCatalogFn(gateCatalog)
	return uc
}

func incompleteGraph() workflow.Graph {
	return workflow.Graph{
		Nodes: []workflow.Node{
			{ID: "t", Type: workflow.NodeTypeTriggerMessageReceived, Config: map[string]interface{}{}},
			{ID: "lbl", Type: workflow.NodeTypeActionAssignLabel, Config: map[string]interface{}{"label_id": "gone"}},
			{ID: "lbl-empty", Type: workflow.NodeTypeActionAssignLabel, Config: map[string]interface{}{"label_id": ""}},
		},
	}
}

func issueCodes(issues []workflow.LintIssue) map[workflow.LintIssueCode][]string {
	codes := map[workflow.LintIssueCode][]string{}
	for _, i := range issues {
		if i.Severity != workflow.SeverityBlocking {
			continue
		}
		codes[i.Code] = append(codes[i.Code], i.NodeID)
	}
	return codes
}

func TestPublishReportsEveryBlockingProblemAtOnce(t *testing.T) {
	repo := NewMockWorkflowRepository()
	storedWorkflow(t, repo, workflow.WorkflowStatusDraft, incompleteGraph())
	uc := gate(repo)
	uc.SetLabelRepo(missingLabels{})

	_, err := uc.Publish(publisherCtx(), "wf-1")
	blocked, ok := workflow.NotPublishableOf(err)
	if !ok {
		t.Fatalf("err = %v, want every blocking problem reported together", err)
	}
	codes := issueCodes(blocked.Issues)
	for _, want := range []workflow.LintIssueCode{workflow.LintTriggerNotConnected, workflow.LintNoEnd, workflow.LintNodeNoIncoming, workflow.LintNodeNoOutgoing, workflow.LintMissingRequiredField, workflow.LintInvalidResourceRef} {
		if len(codes[want]) == 0 {
			t.Errorf("%s missing from %v", want, codes)
		}
	}
	if got := codes[workflow.LintInvalidResourceRef]; len(got) != 1 || got[0] != "lbl" {
		t.Errorf("label references reported on %v, want only the node whose label is gone", got)
	}
	if got := codes[workflow.LintMissingRequiredField]; len(got) != 1 || got[0] != "lbl-empty" {
		t.Errorf("missing fields reported on %v, want the node with the empty label", got)
	}
	if repo.VersionCount("wf-1") != 0 {
		t.Fatal("a draft with blocking problems must not be published")
	}
}

func TestThePublishGateAndTheAlertsPanelAgreeOnTheGraphBlockers(t *testing.T) {
	repo := NewMockWorkflowRepository()
	storedWorkflow(t, repo, workflow.WorkflowStatusDraft, incompleteGraph())

	_, err := gate(repo).Publish(publisherCtx(), "wf-1")
	blocked, ok := workflow.NotPublishableOf(err)
	if !ok {
		t.Fatalf("err = %v", err)
	}
	stored, _ := repo.FindByID("wf-1")
	lint := LintWorkflowGraph(gateCatalog(), stored).Blocking()
	if len(blocked.Issues) != len(lint) {
		t.Fatalf("publish issues = %d, lint blockers = %d: without reference lookups they must be the same list", len(blocked.Issues), len(lint))
	}
	for i := range lint {
		if blocked.Issues[i] != lint[i] {
			t.Fatalf("issue %d = %+v, lint = %+v", i, blocked.Issues[i], lint[i])
		}
	}
}

func TestPublishFailsClosedWhenAReferenceCannotBeChecked(t *testing.T) {
	g := manualGraph("end")
	g.Nodes = append(g.Nodes, workflow.Node{ID: "deal", Type: workflow.NodeTypeActionManageOpportunity, Config: map[string]interface{}{"pipeline_id": "p1"}})
	g.Edges = []workflow.Edge{{Source: "t", Target: "deal"}, {Source: "deal", Target: "end"}}
	repo := NewMockWorkflowRepository()
	storedWorkflow(t, repo, workflow.WorkflowStatusDraft, g)
	uc := gate(repo)
	uc.SetDealFunnels(unreachableFunnels{})

	_, err := uc.Publish(publisherCtx(), "wf-1")
	if err == nil || workflow.IsValidationError(err) {
		t.Fatalf("err = %v, want the lookup failure itself so nothing publishes unchecked", err)
	}
	if repo.VersionCount("wf-1") != 0 {
		t.Fatal("nothing may be published when a reference could not be checked")
	}
}

func TestSavingAnIncompleteDraftNeverRunsThePublishGate(t *testing.T) {
	repo := NewMockWorkflowRepository()
	storedWorkflow(t, repo, workflow.WorkflowStatusDraft, manualGraph("end"))

	saved, err := NewUpdateWorkflowUseCase(repo).Execute(context.Background(), "wf-1", &workflow.Workflow{Name: "Fluxo", Graph: incompleteGraph()}, revisionOf(repo, "wf-1"))
	if err != nil {
		t.Fatalf("an unconnected trigger, a missing end and empty fields must still save: %v", err)
	}
	if saved.Graph.FindNode("lbl-empty") == nil {
		t.Fatal("the incomplete draft was not stored")
	}
}

func TestThePublishRefusalNamesTheUnconnectedOutputOfANode(t *testing.T) {
	g := manualGraph("end")
	g.Nodes = append(g.Nodes, workflow.Node{ID: "pick", Type: "action_pick", Config: map[string]interface{}{}})
	g.Edges = []workflow.Edge{{Source: "t", Target: "pick"}, {Source: "pick", Target: "end", Label: "sucesso"}}
	repo := NewMockWorkflowRepository()
	storedWorkflow(t, repo, workflow.WorkflowStatusDraft, g)
	uc := gate(repo)
	uc.SetCatalogFn(func() []workflow.NodeDefinition {
		return append(gateCatalog(), workflow.NodeDefinition{
			Type: "action_pick", Category: workflow.NodeCategoryAction, Scopes: []workflow.NodeScope{workflow.NodeScopeShared},
			Outputs: []workflow.HandleDefinition{{ID: "sucesso", Label: "Sucesso"}, {ID: "erro", Label: "Erro"}},
		})
	})

	_, err := uc.Publish(publisherCtx(), "wf-1")
	blocked, ok := workflow.NotPublishableOf(err)
	if !ok {
		t.Fatalf("err = %v, want a refusal listing the unconnected output", err)
	}
	var paths []string
	for _, issue := range blocked.Issues {
		if issue.Code == workflow.LintMissingRequiredOutput {
			paths = append(paths, issue.NodeID+"/"+issue.Path)
		}
	}
	if len(paths) != 1 || paths[0] != "pick/erro" {
		t.Fatalf("missing output issues = %v, want pick/erro so the editor names the output", paths)
	}
}
