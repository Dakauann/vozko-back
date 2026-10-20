package node_executors

import (
	"strings"
	"testing"

	aa "vozko/domain/ai_attendance"
	"vozko/domain/workflow"
)

type recordedAttendance struct {
	inputs []aa.StartInput
}

func (r *recordedAttendance) RecordAIReply(in aa.StartInput, _ string) {
	r.inputs = append(r.inputs, in)
}

func TestWorkflowAIAttendanceIsRecordedOnTheConversationsOwnChannel(t *testing.T) {
	for _, entryType := range []string{"whatsapp", "instagram", "telegram", "mercadolivre"} {
		attendance := &recordedAttendance{}
		executor := &aiAgentExecutor{aiAttendance: attendance}
		state := workflow.NewRunState()
		ctx := agentNodeContext(&state, nil)
		ctx.Run.WorkspaceID = "ws-1"
		ctx.Run.EntryID = "entry-1"
		ctx.Run.EntryType = entryType

		executor.recordWorkflowAIAttendance(ctx, "agent-1", "model", "msg-1")

		if len(attendance.inputs) != 1 {
			t.Fatalf("entry type %q: recorded %d replies, want 1", entryType, len(attendance.inputs))
		}
		if got := attendance.inputs[0].Channel; got != entryType {
			t.Fatalf("entry type %q: channel = %q, want the conversation's own channel", entryType, got)
		}
	}
}

func TestTheAgentNodeRefusesARunWithoutAnEntryType(t *testing.T) {
	for _, entryType := range []string{"", "   "} {
		service := &recordingAgentAI{}
		executor := NewAIAgentExecutor(service, nil, &entryTranscript{history: agentNodeTranscript(2)}, nil, nil, nil)
		state := workflow.NewRunState()
		ctx := agentNodeContext(&state, nil)
		ctx.Run.EntryType = entryType

		result, err := executor.Execute(ctx)
		if err != nil {
			t.Fatalf("entry type %q: err = %v, want a node refusal the engine does not retry", entryType, err)
		}
		if result == nil || !strings.Contains(result.Error, "entry type") {
			t.Fatalf("entry type %q: result = %+v, want a clear refusal", entryType, result)
		}
		if len(service.inputs) != 0 {
			t.Fatalf("entry type %q: the model was called %d times, want none", entryType, len(service.inputs))
		}
	}
}
