package workflow_usecase

import (
	"strings"
	"testing"

	"vozko/domain/workflow"
)

func TestEditingAWorkflowScopesTheBuilderLoopToThatWorkflow(t *testing.T) {
	repo := NewMockWorkflowRepository()
	if err := repo.Create(&workflow.Workflow{ID: "wf-1", WorkspaceID: "ws-1"}); err != nil {
		t.Fatal(err)
	}
	uc := &aiBuilderUC{deps: AIBuilderUseCaseDeps{WorkflowRepo: repo}}
	st, err := uc.initState("wf-1", "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	cfg := uc.builderConfig(st)
	if cfg.SessionID != "workflow_builder:wf-1" || cfg.BillingReference != "workflow_builder:wf-1" {
		t.Fatalf("session %q, billing %q", cfg.SessionID, cfg.BillingReference)
	}
}

func TestEachNewWorkflowSessionGetsItsOwnBuilderScope(t *testing.T) {
	uc := &aiBuilderUC{}
	first, err := uc.initState("", "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := uc.initState("", "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	a, b := uc.builderConfig(first), uc.builderConfig(second)
	if !strings.HasPrefix(a.SessionID, "workflow_builder:") || len(a.SessionID) == len("workflow_builder:") {
		t.Fatalf("session %q", a.SessionID)
	}
	if a.BillingReference != a.SessionID {
		t.Fatalf("billing %q, session %q", a.BillingReference, a.SessionID)
	}
	if a.SessionID == b.SessionID {
		t.Fatal("two drafts must not share a builder scope")
	}
}
