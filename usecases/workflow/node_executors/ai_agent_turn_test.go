package node_executors

import (
	"testing"

	"vozko/domain/ai"
	"vozko/domain/workflow"
)

func TestTheAgentNodeNamesItsTurnSoRoutedToolCallsCountOnTheSameSessionAsItsReplies(t *testing.T) {
	service := &recordingAgentAI{}
	executor := NewAIAgentExecutor(service, nil, &entryTranscript{history: agentNodeTranscript(2)}, nil, nil, nil)
	state := workflow.NewRunState()
	ctx := agentNodeContext(&state, nil)

	if _, err := executor.Execute(ctx); err != nil {
		t.Fatal(err)
	}
	if len(service.inputs) == 0 {
		t.Fatal("the model was never called")
	}
	want := ai.AgentTurn{EntryID: "entry-1", EntryType: "telegram", AgentID: "wf-1"}
	if got := service.inputs[0].AgentTurn; got != want {
		t.Fatalf("agent turn = %+v, want %+v (prompt mode is attributed to the workflow, like its replies)", got, want)
	}
}

func TestTheAgentNodeNeverRunsAgentToolsSoEveryStageMoveInARunGoesThroughTheMoveNodeAndItsCause(t *testing.T) {
	service := &recordingAgentAI{}
	executor := NewAIAgentExecutor(service, nil, &entryTranscript{history: agentNodeTranscript(2)}, nil, nil, nil)
	state := workflow.NewRunState()
	ctx := agentNodeContext(&state, map[string]interface{}{
		"custom_tools": []interface{}{map[string]interface{}{"name": "manage_entry_stage", "description": "mover a conversa de etapa"}},
	})

	if _, err := executor.Execute(ctx); err != nil {
		t.Fatal(err)
	}
	if len(service.inputs) == 0 {
		t.Fatal("the model was never called")
	}
	input := service.inputs[0]
	if input.ExecutionModeOrDefault() != ai.ToolExecutionModeNone || len(input.ToolConfigs) != 0 {
		t.Fatalf("mode %q with tool configs %v: a tool call from the agent node must only route to an edge, never run a registry tool without the run's cause", input.ExecutionModeOrDefault(), input.ToolConfigs)
	}
}
