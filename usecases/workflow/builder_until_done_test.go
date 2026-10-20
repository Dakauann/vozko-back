package workflow_usecase

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/ai"
	"vozko/domain/workflow"
)

func catalogedBuilderModel() ai.ModelInfo {
	return ai.ModelInfo{ID: "anthropic/claude-sonnet-4", ContextLength: 200000, PromptPrice: 0.000003}
}

func TestTheBuilderIteratesUntilDoneWithTheModelWindowLikeElo(t *testing.T) {
	uc := &aiBuilderUC{deps: AIBuilderUseCaseDeps{MaxTokens: 1_000_000}}
	cfg := uc.builderConfig(&builderState{model: "anthropic/claude-sonnet-4", modelInfo: catalogedBuilderModel()})
	if !cfg.IterateUntilDone || cfg.MaxIterations != 0 {
		t.Fatalf("iterate=%v max=%d, want no step cap", cfg.IterateUntilDone, cfg.MaxIterations)
	}
	if cfg.ModelLimits.ID != "anthropic/claude-sonnet-4" || cfg.SessionTokenBudget != 0 || cfg.MaxHistoryMsgs != 0 {
		t.Fatalf("limits=%q budget=%d history=%d, want the model window with compaction", cfg.ModelLimits.ID, cfg.SessionTokenBudget, cfg.MaxHistoryMsgs)
	}
}

func TestTheBuilderKeepsATokenBudgetOnlyForModelsTheCatalogDoesNotKnow(t *testing.T) {
	uc := &aiBuilderUC{deps: AIBuilderUseCaseDeps{MaxTokens: 1_000_000}}
	cfg := uc.builderConfig(&builderState{model: "lab/unknown"})
	if !cfg.IterateUntilDone || cfg.SessionTokenBudget != 1_000_000 {
		t.Fatalf("iterate=%v budget=%d, want until done with the fallback budget", cfg.IterateUntilDone, cfg.SessionTokenBudget)
	}
}

func TestTheModelGuardKeepsTheResolvedModelForTheLoop(t *testing.T) {
	uc := &aiBuilderUC{deps: AIBuilderUseCaseDeps{AIService: catalogAI{models: []ai.ModelInfo{builderThinker()}}}}
	st := &builderState{model: "lab/thinker"}
	if !uc.guardModel(context.Background(), func(string, interface{}) {}, st) {
		t.Fatal("a cataloged model with tools must be allowed")
	}
	if st.modelInfo.ID != "lab/thinker" {
		t.Fatalf("modelInfo = %q, want the resolved catalog entry", st.modelInfo.ID)
	}
}

func TestTheRegroundNamesTheIterationWithoutABudgetWhenThereIsNoCap(t *testing.T) {
	uc := &aiBuilderUC{}
	msg := uc.regroundMessage(&builderState{graph: &workflow.Graph{}}, 7, 0, 0)
	if !strings.Contains(msg, "ITERAÇÃO 7") || strings.Contains(msg, "orçamento limitado") || strings.Contains(msg, " de 0") {
		t.Fatalf("reground = %q, want the iteration without a cap", msg)
	}
}
