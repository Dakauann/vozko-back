package ai_attendance_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/ai"
	aa "vozko/domain/ai_attendance"
)

type scriptedAI struct {
	ai.Service
	out *ai.GenerateOutput
	err error
}

func (s scriptedAI) Generate(context.Context, ai.GenerateInput) (*ai.GenerateOutput, error) {
	return s.out, s.err
}

type recordedToolCalls struct {
	in    aa.StartInput
	count int
}

type toolCallSpy struct {
	calls []recordedToolCalls
}

func (s *toolCallSpy) RecordToolCalls(in aa.StartInput, count int) {
	s.calls = append(s.calls, recordedToolCalls{in: in, count: count})
}

func agentTurnInput() ai.GenerateInput {
	return ai.GenerateInput{
		WorkspaceID: "ws-1",
		Model:       "model-x",
		AgentTurn:   ai.AgentTurn{EntryID: "entry-1", EntryType: "instagram", AgentID: "agent-1", CampaignID: "camp-1"},
	}
}

func twoToolCalls() *ai.GenerateOutput {
	return &ai.GenerateOutput{ToolCalls: []ai.ToolCall{{Name: "http_request"}, {Name: "transfer_to_human"}}}
}

func TestEveryAgentTurnCountsItsToolCallsOnTheAttendanceSessionWhateverTheChannel(t *testing.T) {
	spy := &toolCallSpy{}
	counted := counting(scriptedAI{out: twoToolCalls()}, spy)

	if _, err := counted.Generate(context.Background(), agentTurnInput()); err != nil {
		t.Fatal(err)
	}
	if len(spy.calls) != 1 || spy.calls[0].count != 2 {
		t.Fatalf("recorded %+v, want one record of two tool calls", spy.calls)
	}
	want := aa.StartInput{WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: "instagram", AgentID: "agent-1", Channel: "instagram", CampaignID: "camp-1", Model: "model-x"}
	if spy.calls[0].in != want {
		t.Fatalf("session input = %+v, want %+v", spy.calls[0].in, want)
	}
}

func TestToolCallsOutsideAnIdentifiedAgentTurnAreNotCounted(t *testing.T) {
	notATurn := agentTurnInput()
	notATurn.AgentTurn = ai.AgentTurn{}
	noAgent := agentTurnInput()
	noAgent.AgentTurn.AgentID = ""
	noWorkspace := agentTurnInput()
	noWorkspace.WorkspaceID = ""

	cases := []struct {
		name  string
		input ai.GenerateInput
		inner scriptedAI
	}{
		{"background generation", notATurn, scriptedAI{out: twoToolCalls()}},
		{"turn without an agent", noAgent, scriptedAI{out: twoToolCalls()}},
		{"turn without a workspace", noWorkspace, scriptedAI{out: twoToolCalls()}},
		{"no tool calls", agentTurnInput(), scriptedAI{out: &ai.GenerateOutput{}}},
		{"generation failed", agentTurnInput(), scriptedAI{out: twoToolCalls(), err: errors.New("provider down")}},
		{"no output", agentTurnInput(), scriptedAI{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			spy := &toolCallSpy{}
			_, _ = counting(c.inner, spy).Generate(context.Background(), c.input)
			if len(spy.calls) != 0 {
				t.Fatalf("unexpected record: %+v", spy.calls)
			}
		})
	}
}

func TestCountingKeepsTheGenerationResultUntouched(t *testing.T) {
	out := twoToolCalls()
	got, err := counting(scriptedAI{out: out}, nil).Generate(context.Background(), agentTurnInput())
	if err != nil || got != out {
		t.Fatalf("got %v, %v; want the inner output", got, err)
	}
}

func TestRecordToolCallsAddsToTheOpenSessionAndOpensOneForTheFirstTurn(t *testing.T) {
	repo := newMem()
	svc := NewSessionService(repo, nil)
	in := aa.StartInput{WorkspaceID: "ws", EntryID: "e1", EntryType: "telegram", AgentID: "ag1", Channel: "telegram"}

	svc.RecordToolCalls(in, 2)
	svc.RecordToolCalls(in, 1)
	svc.RecordToolCalls(in, 0)

	open, _ := repo.FindOpenByEntry("ws", "e1", "telegram")
	if open == nil {
		t.Fatal("the first tool call must open the attendance session")
	}
	if repo.toolCalls[open.ID] != 3 {
		t.Fatalf("tool calls = %d, want 3", repo.toolCalls[open.ID])
	}
}
