package workflow

import (
	"strings"
	"testing"
	"time"
)

func agentRun(status RunStatus, result map[string]interface{}) *WorkflowRun {
	return &WorkflowRun{
		ID: "run-1", WorkflowID: "wf-1", WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: "whatsapp",
		Status: status, StartedBy: StartedByAgent("agent-7"), AgentResult: result, Error: "boom",
		UpdatedAt: time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC),
	}
}

func TestOnlyAFinishedAgentStartedRunAnnouncesItsEnd(t *testing.T) {
	cases := []struct {
		name string
		run  *WorkflowRun
		want bool
	}{
		{"a completed agent run", agentRun(RunStatusCompleted, nil), true},
		{"a failed agent run", agentRun(RunStatusError, nil), true},
		{"a cancelled agent run", agentRun(RunStatusCancelled, nil), true},
		{"a running agent run has not ended", agentRun(RunStatusRunning, nil), false},
		{"a waiting agent run has not ended", agentRun(RunStatusWaiting, nil), false},
		{"a trigger run is nobody's tool call", &WorkflowRun{ID: "r", Status: RunStatusCompleted, StartedBy: StartedByTrigger(TriggerMessageReceived)}, false},
		{"an agent prefix without an agent id is unusable", &WorkflowRun{ID: "r", Status: RunStatusCompleted, StartedBy: "agent:"}, false},
		{"no run", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := AgentRunFinishedOf(tc.run); ok != tc.want {
				t.Fatalf("AgentRunFinishedOf ok = %v, want %v", ok, tc.want)
			}
		})
	}
}

func TestTheFinishedEventCarriesTheRunAndItsAgent(t *testing.T) {
	ev, ok := AgentRunFinishedOf(agentRun(RunStatusCompleted, map[string]interface{}{"status": "ok"}))
	if !ok {
		t.Fatal("a completed agent run must announce its end")
	}
	want := AgentRunFinished{
		RunID: "run-1", WorkflowID: "wf-1", WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: "whatsapp",
		AgentID: "agent-7", Status: RunStatusCompleted, HasAgentResult: true,
		FinishedAt: time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC),
	}
	if ev != want {
		t.Fatalf("event = %+v, want %+v", ev, want)
	}
}

func TestTheAgentContinuesOnlyAfterAResultOrAFailure(t *testing.T) {
	cases := []struct {
		name string
		ev   AgentRunFinished
		want ContinuationKind
	}{
		{"the workflow replied to the agent", AgentRunFinished{Status: RunStatusCompleted, HasAgentResult: true}, ContinuationResult},
		{"the workflow handled the conversation itself", AgentRunFinished{Status: RunStatusCompleted}, ContinuationNone},
		{"the workflow failed", AgentRunFinished{Status: RunStatusError}, ContinuationFailure},
		{"the workflow failed after replying still reports the failure", AgentRunFinished{Status: RunStatusError, HasAgentResult: true}, ContinuationFailure},
		{"an operator, a pause or a finish cancelled it", AgentRunFinished{Status: RunStatusCancelled, HasAgentResult: true}, ContinuationNone},
		{"a run still going is not a continuation", AgentRunFinished{Status: RunStatusRunning}, ContinuationNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.ev.Continuation(); got != tc.want {
				t.Fatalf("Continuation() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTheContinuationNoteTellsTheModelWhatHappened(t *testing.T) {
	result := ContinuationNote(ContinuationResult, "Matrícula", "")
	for _, want := range []string{`"Matrícula"`, "retornou", "Continue a conversa"} {
		if !strings.Contains(result, want) {
			t.Errorf("result note %q misses %q", result, want)
		}
	}
	failure := ContinuationNote(ContinuationFailure, "Matrícula", "o fluxo ficou parado")
	for _, want := range []string{`"Matrícula"`, "falhou", "o fluxo ficou parado", "não invente"} {
		if !strings.Contains(failure, want) {
			t.Errorf("failure note %q misses %q", failure, want)
		}
	}
	if note := ContinuationNote(ContinuationNone, "Matrícula", ""); note != "" {
		t.Errorf("no continuation must have no note, got %q", note)
	}
}

func TestARunFailureIsToldInPlainPortuguese(t *testing.T) {
	cases := []struct {
		name, err, want string
	}{
		{"a stuck run", "run stuck: not updated within timeout", "parado"},
		{"the step guard", ErrMaxExecutionsReached.Error(), "repetir etapas"},
		{"the lifetime guard", ErrDurableExecutionsReached.Error(), "repetir etapas"},
		{"a loop", "loop detected: node n3 visited 4 times without waiting", "repetir etapas"},
		{"an unreadable switch", ErrAutomationStateUnreadable.Error() + ": entry e (whatsapp)", "automação"},
		{"an edited graph", "node not found: n9", "alterado"},
		{"nothing recorded", "  ", "erro"},
		{"a node error keeps its detail", "HTTP 500 do servidor", "HTTP 500 do servidor"},
		{"a secret in the detail is masked", `login falhou {"token": "abc123secret"}`, "••••"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := HumanRunFailure(tc.err)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("HumanRunFailure(%q) = %q, want it to contain %q", tc.err, got, tc.want)
			}
			if strings.Contains(got, "abc123secret") {
				t.Fatalf("HumanRunFailure leaked a secret: %q", got)
			}
		})
	}
}
