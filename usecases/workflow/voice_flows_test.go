package workflow_usecase

import (
	"errors"
	"testing"

	"vozko/domain/workflow"
)

func activeVoiceWorkflow(repo *MockWorkflowRepository, id, trunkID string) *workflow.Workflow {
	w := voiceWorkflow(id, trunkID, "greeting")
	w.Status = workflow.WorkflowStatusActive
	w.Normalize()
	_ = repo.Create(w)
	return w
}

func TestTheTrunksActiveVoiceWorkflowAnswersTheCall(t *testing.T) {
	repo := NewMockWorkflowRepository()
	activeVoiceWorkflow(repo, "wf-other", "trunk-2")
	activeVoiceWorkflow(repo, "wf-main", "trunk-1")
	flows := NewInboundVoiceFlows(repo, NewMockWorkflowRunRepository(), voiceEngine(NewMockWorkflowRunRepository()))

	flow, err := flows.FlowFor("ws1", "trunk-1")
	if err != nil || flow == nil || flow.ID != "wf-main" {
		t.Fatalf("FlowFor = %v, %v", flow, err)
	}
	none, err := flows.FlowFor("ws1", "trunk-9")
	if err != nil || none != nil {
		t.Fatalf("a trunk without a voice workflow got %v, %v", none, err)
	}
}

func TestTwoActiveVoiceWorkflowsOnOneTrunkRefuseTheCall(t *testing.T) {
	repo := NewMockWorkflowRepository()
	activeVoiceWorkflow(repo, "wf-a", "trunk-1")
	activeVoiceWorkflow(repo, "wf-b", "trunk-1")
	flows := NewInboundVoiceFlows(repo, NewMockWorkflowRunRepository(), voiceEngine(NewMockWorkflowRunRepository()))

	if _, err := flows.FlowFor("ws1", "trunk-1"); !errors.Is(err, workflow.ErrVoiceFlowAmbiguous) {
		t.Fatalf("err = %v, want ErrVoiceFlowAmbiguous", err)
	}
}

func TestAnsweringRecordsARunForTheCallAndRunsItToTheEnd(t *testing.T) {
	repo := NewMockWorkflowRepository()
	flow := activeVoiceWorkflow(repo, "wf-main", "trunk-1")
	runs := NewMockWorkflowRunRepository()
	flows := NewInboundVoiceFlows(repo, runs, voiceEngine(runs))

	err := flows.Answer(flow, workflow.InboundVoiceCall{
		WorkspaceID: "ws1", TrunkID: "trunk-1", CallID: "sip-in-1",
		CallerNumber: "5584994409684", CalledNumber: "8433221100",
		Call: &scriptedCaller{keys: []rune{'1'}},
	})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	recorded := runs.All()
	if len(recorded) != 1 {
		t.Fatalf("runs = %d", len(recorded))
	}
	run := recorded[0]
	if run.EntryID != "sip-in-1" || run.EntryType != workflow.EntryTypeSIPCall || run.Status != workflow.RunStatusCompleted {
		t.Fatalf("run = %+v", run)
	}
	if run.State.GetString("caller_number") != "5584994409684" || run.State.GetString("called_number") != "8433221100" {
		t.Fatalf("state = %v", run.State.Vars)
	}
}

func TestAMessagingWorkflowNeverAnswersACall(t *testing.T) {
	repo := NewMockWorkflowRepository()
	flows := NewInboundVoiceFlows(repo, NewMockWorkflowRunRepository(), voiceEngine(NewMockWorkflowRunRepository()))
	messaging := &workflow.Workflow{ID: "wf-msg", WorkspaceID: "ws1", Type: workflow.WorkflowTypeMessages}
	if err := flows.Answer(messaging, workflow.InboundVoiceCall{WorkspaceID: "ws1", Call: &scriptedCaller{}}); err == nil {
		t.Fatal("a messaging workflow ran on a call")
	}
}

func (m *MockWorkflowRunRepository) All() []*workflow.WorkflowRun {
	m.mu.Lock()
	defer m.mu.Unlock()
	all := make([]*workflow.WorkflowRun, 0, len(m.runs))
	for _, run := range m.runs {
		cp := *run
		all = append(all, &cp)
	}
	return all
}
