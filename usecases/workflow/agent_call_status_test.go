package workflow_usecase

import (
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/workflow"
)

type agentRunsStub struct {
	run       *workflow.AgentCallRun
	err       error
	asked     []string
	cancelled []workflow.CancelledAgentRun
	cancelErr error
	reasons   []string
}

func (s *agentRunsStub) LatestForEntry(workspaceID, entryID, entryType string) (*workflow.AgentCallRun, error) {
	s.asked = append(s.asked, workspaceID+"/"+entryID+"/"+entryType)
	return s.run, s.err
}

func (s *agentRunsStub) CancelActiveAgentRuns(entryID, entryType, reason string) ([]workflow.CancelledAgentRun, error) {
	s.reasons = append(s.reasons, entryID+"/"+entryType+"/"+reason)
	return s.cancelled, s.cancelErr
}

func stepNamesRegistry() *NodeExecutorRegistry {
	registry := NewNodeExecutorRegistry()
	registry.RegisterDefinition(workflow.NodeDefinition{Type: workflow.NodeTypeActionHTTPRequest, Label: "Requisição HTTP"})
	return registry
}

func TestTheAgentContextLineNamesTheStepByItsCatalogLabel(t *testing.T) {
	now := time.Date(2026, 10, 19, 20, 30, 0, 0, time.UTC)
	runs := &agentRunsStub{run: &workflow.AgentCallRun{
		WorkflowName: "Matrícula", Status: workflow.RunStatusRunning, StartedAt: now.Add(-2 * time.Minute),
		CurrentNodeType: workflow.NodeTypeActionHTTPRequest,
	}}
	status, err := NewAgentCallStatus(runs, stepNamesRegistry())
	if err != nil {
		t.Fatal(err)
	}
	status.now = func() time.Time { return now }

	line := status.ContextLine("ws-1", "e-1", "whatsapp")
	if !strings.Contains(line, `Fluxo "Matrícula" em execução há 2 min, etapa: Requisição HTTP`) {
		t.Fatalf("line = %q", line)
	}
	if len(runs.asked) != 1 || runs.asked[0] != "ws-1/e-1/whatsapp" {
		t.Fatalf("asked = %v", runs.asked)
	}
}

func TestAnUnreadableOrMissingAgentRunGivesNoLine(t *testing.T) {
	for name, runs := range map[string]*agentRunsStub{
		"none":       {},
		"unreadable": {err: errors.New("db down")},
	} {
		t.Run(name, func(t *testing.T) {
			status, _ := NewAgentCallStatus(runs, stepNamesRegistry())
			if line := status.ContextLine("ws-1", "e-1", "whatsapp"); line != "" {
				t.Fatalf("line = %q", line)
			}
		})
	}
	status, _ := NewAgentCallStatus(&agentRunsStub{}, stepNamesRegistry())
	if line := status.ContextLine("", "e-1", "whatsapp"); line != "" {
		t.Fatalf("a turn without a conversation must not ask: %q", line)
	}
}

func TestAStepWithoutACatalogLabelIsNamedByItsType(t *testing.T) {
	if got := stepNamesRegistry().StepName(workflow.NodeTypeWaitForReply); got != string(workflow.NodeTypeWaitForReply) {
		t.Fatalf("StepName = %q", got)
	}
	if got := stepNamesRegistry().StepName(workflow.NodeTypeActionHTTPRequest); got != "Requisição HTTP" {
		t.Fatalf("StepName = %q", got)
	}
}

func TestFinishingAConversationCancelsItsAgentRunsAndTracesWhy(t *testing.T) {
	runs := &agentRunsStub{cancelled: []workflow.CancelledAgentRun{
		{RunID: "r-1", WorkspaceID: "ws-1", WorkflowID: "wf-1", CurrentNodeID: "n7"},
		{RunID: "r-2", WorkspaceID: "ws-1", WorkflowID: "wf-2", CurrentNodeID: "n2"},
	}}
	logs := NewMockWorkflowRunLogRepository()
	canceller, err := NewAgentRunFinishCanceller(runs, logs)
	if err != nil {
		t.Fatal(err)
	}

	if err := canceller.CancelAgentRunsOfFinishedConversation("e-1", "whatsapp"); err != nil {
		t.Fatal(err)
	}
	if len(runs.reasons) != 1 || runs.reasons[0] != "e-1/whatsapp/conversation_finished" {
		t.Fatalf("cancel calls = %v", runs.reasons)
	}
	for _, runID := range []string{"r-1", "r-2"} {
		steps, _ := logs.FindByRunID(runID)
		if len(steps) != 1 {
			t.Fatalf("steps of %s = %+v", runID, steps)
		}
		step := steps[0]
		if step.Status != workflow.LogStatusCancelled || step.ErrorKind != workflow.ErrorKindCancelled || step.Error != workflow.CancelReasonConversationFinished || step.NodeID == "" || step.StartedAt == nil {
			t.Fatalf("step = %+v", step)
		}
	}
}

func TestACancelFailureIsReturnedAndWritesNoStep(t *testing.T) {
	logs := NewMockWorkflowRunLogRepository()
	canceller, _ := NewAgentRunFinishCanceller(&agentRunsStub{cancelErr: errors.New("db down")}, logs)
	if err := canceller.CancelAgentRunsOfFinishedConversation("e-1", "whatsapp"); err == nil {
		t.Fatal("expected the cancel failure")
	}
	if len(logs.logs) != 0 {
		t.Fatalf("steps = %+v", logs.logs)
	}
}

func TestTheAgentRunPiecesRefuseMissingDependencies(t *testing.T) {
	if _, err := NewAgentCallStatus(nil, stepNamesRegistry()); err == nil {
		t.Fatal("status needs the read model")
	}
	if _, err := NewAgentCallStatus(&agentRunsStub{}, nil); err == nil {
		t.Fatal("status needs the step names")
	}
	if _, err := NewAgentRunFinishCanceller(nil, NewMockWorkflowRunLogRepository()); err == nil {
		t.Fatal("the canceller needs the runs")
	}
	if _, err := NewAgentRunFinishCanceller(&agentRunsStub{}, nil); err == nil {
		t.Fatal("the canceller needs the step log")
	}
}
