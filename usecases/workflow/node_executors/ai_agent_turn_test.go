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
