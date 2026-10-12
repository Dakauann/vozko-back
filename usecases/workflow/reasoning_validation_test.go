package workflow_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/ai"
	"vozko/domain/workflow"
)

type brokenCatalog struct{}

func (brokenCatalog) GetModelsWithPricing(context.Context) ([]ai.ModelInfo, error) {
	return nil, errors.New("catalog down")
}

type staticCatalog []ai.ModelInfo

func (s staticCatalog) GetModelsWithPricing(context.Context) ([]ai.ModelInfo, error) {
	return s, nil
}

func reasoningGraph(level string) workflow.Graph {
	return workflow.Graph{Nodes: []workflow.Node{{
		ID:   "ai",
		Type: workflow.NodeTypeActionAIAgent,
		Config: map[string]interface{}{
			"source":                         "prompt",
			"model":                          "lab/thinker",
			"instructions":                   "x",
			workflow.ConfigKeyReasoningLevel: level,
		},
	}}}
}

func TestGraphReasoningCheckFailsClosed(t *testing.T) {
	offered := staticCatalog{builderThinker()}
	cases := []struct {
		name    string
		catalog ModelCatalog
		level   string
		err     error
	}{
		{name: "an offered level passes", catalog: offered, level: "high"},
		{name: "no level needs no catalog", catalog: nil, level: ""},
		{name: "a level outside the offer is refused", catalog: offered, level: "max", err: workflow.ErrNodeReasoningLevelUnsupported},
		{name: "a missing catalog refuses a level", catalog: nil, level: "high", err: workflow.ErrNodeReasoningLevelUnsupported},
		{name: "a broken catalog refuses a level", catalog: brokenCatalog{}, level: "high", err: workflow.ErrNodeReasoningLevelUnsupported},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := reasoningGraph(tc.level)
			if err := checkGraphReasoning(context.Background(), tc.catalog, &g); !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
}

type requestKey struct{}

type recordingCatalog struct {
	staticCatalog
	seen *[]interface{}
}

func (r recordingCatalog) GetModelsWithPricing(ctx context.Context) ([]ai.ModelInfo, error) {
	*r.seen = append(*r.seen, ctx.Value(requestKey{}))
	return r.staticCatalog, nil
}

func TestSavingAWorkflowAsksTheCatalogWithTheRequestContext(t *testing.T) {
	repo := NewMockWorkflowRepository()
	if err := repo.Create(&workflow.Workflow{ID: "wf-1", WorkspaceID: "ws-1", Name: "Fluxo", Status: workflow.WorkflowStatusDraft, Type: workflow.WorkflowTypeMessages}); err != nil {
		t.Fatal(err)
	}
	var seen []interface{}
	uc := NewUpdateWorkflowUseCase(repo).(*updateWorkflowUseCase)
	uc.SetModelCatalog(recordingCatalog{staticCatalog: staticCatalog{builderThinker()}, seen: &seen})
	ctx := context.WithValue(context.Background(), requestKey{}, "req-1")
	if _, err := uc.Execute(ctx, "wf-1", &workflow.Workflow{Name: "Fluxo", Type: workflow.WorkflowTypeMessages, Graph: reasoningGraph("high")}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] != "req-1" {
		t.Fatalf("catalog contexts = %v, want the request context", seen)
	}
}

func TestSavingAWorkflowChecksTheReasoningLevel(t *testing.T) {
	repo := NewMockWorkflowRepository()
	if err := repo.Create(&workflow.Workflow{ID: "wf-1", WorkspaceID: "ws-1", Name: "Fluxo", Status: workflow.WorkflowStatusDraft, Type: workflow.WorkflowTypeMessages}); err != nil {
		t.Fatal(err)
	}
	uc := NewUpdateWorkflowUseCase(repo).(*updateWorkflowUseCase)
	uc.SetModelCatalog(staticCatalog{builderThinker()})

	refused := &workflow.Workflow{Name: "Fluxo", Type: workflow.WorkflowTypeMessages, Graph: reasoningGraph("max")}
	if _, err := uc.Execute(context.Background(), "wf-1", refused); !errors.Is(err, workflow.ErrNodeReasoningLevelUnsupported) {
		t.Fatalf("save with an unsupported level err = %v, want refusal", err)
	}
	saved, _ := repo.FindByID("wf-1")
	if len(saved.Graph.Nodes) != 0 {
		t.Fatal("a refused save must not store the graph")
	}

	accepted := &workflow.Workflow{Name: "Fluxo", Type: workflow.WorkflowTypeMessages, Graph: reasoningGraph("high")}
	if _, err := uc.Execute(context.Background(), "wf-1", accepted); err != nil {
		t.Fatalf("save with an offered level err = %v", err)
	}
}
