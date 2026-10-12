package node_executors

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/ai"
	"vozko/domain/workflow"
)

type catalogAgentAI struct {
	recordingAgentAI
	models []ai.ModelInfo
}

func (c *catalogAgentAI) Generate(ctx context.Context, in ai.GenerateInput) (*ai.GenerateOutput, error) {
	return c.recordingAgentAI.Generate(ctx, in)
}

func (c *catalogAgentAI) GetModelsWithPricing(context.Context) ([]ai.ModelInfo, error) {
	return c.models, nil
}

func agentThinker(efforts ...string) ai.ModelInfo {
	return ai.DescribeModel(ai.ModelFacts{
		ID:         "openai/gpt-4o-mini",
		Parameters: []string{"tools", "reasoning"},
		Reasoning:  &ai.ReasoningFacts{Efforts: efforts, Default: efforts[0]},
	})
}

func TestTheAgentNodeSendsTheChosenReasoningLevel(t *testing.T) {
	service := &catalogAgentAI{models: []ai.ModelInfo{agentThinker("low", "high")}}
	executor := NewAIAgentExecutor(service, nil, &entryTranscript{history: agentNodeTranscript(2)}, nil, nil, nil)
	state := workflow.NewRunState()
	if _, err := executor.Execute(agentNodeContext(&state, map[string]interface{}{workflow.ConfigKeyReasoningLevel: "high"})); err != nil {
		t.Fatal(err)
	}
	if len(service.inputs) != 1 || service.inputs[0].ReasoningEffort != ai.ReasoningHigh {
		t.Fatalf("inputs = %+v, want effort high", service.inputs)
	}
}

func TestTheAgentNodeWithoutALevelLeavesTheModelDefault(t *testing.T) {
	service := &catalogAgentAI{}
	executor := NewAIAgentExecutor(service, nil, &entryTranscript{history: agentNodeTranscript(2)}, nil, nil, nil)
	state := workflow.NewRunState()
	if _, err := executor.Execute(agentNodeContext(&state, nil)); err != nil {
		t.Fatal(err)
	}
	if service.inputs[0].ReasoningEffort != "" {
		t.Fatalf("effort = %q, want the model default", service.inputs[0].ReasoningEffort)
	}
}

func TestTheAgentNodeRefusesALevelTheModelNoLongerOffers(t *testing.T) {
	service := &catalogAgentAI{models: []ai.ModelInfo{agentThinker("low", "high")}}
	executor := NewAIAgentExecutor(service, nil, &entryTranscript{history: agentNodeTranscript(2)}, nil, nil, nil)
	config := map[string]interface{}{workflow.ConfigKeyReasoningLevel: "high"}
	state := workflow.NewRunState()
	if _, err := executor.Execute(agentNodeContext(&state, config)); err != nil {
		t.Fatalf("the level was offered when the workflow was activated: %v", err)
	}

	service.models = []ai.ModelInfo{agentThinker("low", "medium")}
	state = workflow.NewRunState()
	_, err := executor.Execute(agentNodeContext(&state, config))
	if !errors.Is(err, workflow.ErrNodeReasoningLevelUnsupported) {
		t.Fatalf("err = %v, want the run to stop on the level the model dropped", err)
	}
	if len(service.inputs) != 1 {
		t.Fatalf("the model was called %d times, want only the call before the catalog changed", len(service.inputs))
	}
}

func TestTheAgentNodeRoutesARefusedLevelToTheErrorEdge(t *testing.T) {
	service := &catalogAgentAI{models: []ai.ModelInfo{agentThinker("low")}}
	executor := NewAIAgentExecutor(service, nil, &entryTranscript{history: agentNodeTranscript(2)}, nil, nil, nil)
	state := workflow.NewRunState()
	nctx := agentNodeContext(&state, map[string]interface{}{workflow.ConfigKeyReasoningLevel: "max"})
	nctx.Graph = &workflow.Graph{Edges: []workflow.Edge{{Source: "ai-1", Target: "fallback", Label: "erro"}}}
	res, err := executor.Execute(nctx)
	if err != nil {
		t.Fatalf("err = %v, want the error edge", err)
	}
	if res.NextNodeID != "fallback" || len(service.inputs) != 0 {
		t.Fatalf("next = %q, calls = %d, want the error edge without calling the model", res.NextNodeID, len(service.inputs))
	}
}
