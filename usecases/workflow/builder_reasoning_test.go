package workflow_usecase

import (
	"context"
	"testing"

	"vozko/domain/ai"
)

type catalogAI struct {
	ai.Service
	models []ai.ModelInfo
	err    error
}

func (c catalogAI) GetModelsWithPricing(context.Context) ([]ai.ModelInfo, error) {
	return c.models, c.err
}

func builderThinker() ai.ModelInfo {
	return ai.DescribeModel(ai.ModelFacts{
		ID:         "lab/thinker",
		Parameters: []string{"tools", "reasoning"},
		Reasoning:  &ai.ReasoningFacts{Efforts: []string{"high", "medium", "low"}, Default: "medium"},
	})
}

func TestBuilderModelChoiceCarriesTheReasoningLevel(t *testing.T) {
	st := &builderState{model: "lab/plain"}
	st.chooseModel(setModelData{Model: " lab/thinker ", Reasoning: " high "})
	if st.model != "lab/thinker" || st.reasoning != ai.ReasoningHigh {
		t.Fatalf("state = %q / %q, want lab/thinker / high", st.model, st.reasoning)
	}
	st.chooseModel(setModelData{Model: "lab/plain"})
	if st.model != "lab/plain" || st.reasoning != "" {
		t.Fatalf("state = %q / %q, want lab/plain with the model default", st.model, st.reasoning)
	}
	st.chooseModel(setModelData{Model: "  ", Reasoning: "low"})
	if st.model != "lab/plain" || st.reasoning != "" {
		t.Fatalf("an empty model must be ignored, got %q / %q", st.model, st.reasoning)
	}
}

func TestBuilderConfigSendsTheChosenLevel(t *testing.T) {
	uc := &aiBuilderUC{}
	cfg := uc.builderConfig(&builderState{model: "lab/thinker", reasoning: ai.ReasoningLow})
	if cfg.ReasoningEffort != ai.ReasoningLow {
		t.Fatalf("effort = %q, want low", cfg.ReasoningEffort)
	}
}

func builderBlindModel() ai.ModelInfo {
	return ai.DescribeModel(ai.ModelFacts{ID: "lab/thinker", Parameters: []string{"reasoning"}})
}

func TestBuilderRefusesAModelItCannotConfirm(t *testing.T) {
	cases := []struct {
		name    string
		service ai.Service
		level   ai.ReasoningLevel
		refusal error
	}{
		{name: "an offered level runs", service: catalogAI{models: []ai.ModelInfo{builderThinker()}}, level: ai.ReasoningHigh},
		{name: "no level runs", service: catalogAI{models: []ai.ModelInfo{builderThinker()}}},
		{name: "a level outside the offer stops", service: catalogAI{models: []ai.ModelInfo{builderThinker()}}, level: ai.ReasoningMax, refusal: ai.ErrReasoningLevelUnsupported},
		{name: "a model without tools stops", service: catalogAI{models: []ai.ModelInfo{builderBlindModel()}}, refusal: ai.ErrModelWithoutTools},
		{name: "a model missing from the catalog stops", service: catalogAI{models: []ai.ModelInfo{}}, refusal: ai.ErrModelWithoutTools},
		{name: "an unreadable catalog stops", service: catalogAI{err: ai.ErrProviderUnavailable}, refusal: ai.ErrModelCatalogUnavailable},
		{name: "no AI service stops", service: nil, refusal: ai.ErrModelCatalogUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uc := &aiBuilderUC{deps: AIBuilderUseCaseDeps{AIService: tc.service}}
			st := &builderState{model: "lab/thinker", reasoning: tc.level}
			var done []donePayload
			emit := func(kind string, payload interface{}) {
				if kind == "done" {
					done = append(done, payload.(donePayload))
				}
			}
			allowed := tc.refusal == nil
			if got := uc.guardModel(context.Background(), emit, st); got != allowed {
				t.Fatalf("guardModel = %v, want %v", got, allowed)
			}
			if allowed && len(done) != 0 {
				t.Fatalf("an allowed model emitted %+v", done)
			}
			if allowed {
				return
			}
			want, _ := ai.RefusalOf(tc.refusal)
			if len(done) != 1 || done[0].Valid || done[0].Code != want.Code || done[0].Summary != want.Message {
				t.Fatalf("done = %+v, want one refusal with code %q", done, want.Code)
			}
		})
	}
}
