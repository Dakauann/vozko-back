package workflow_usecase

import (
	"errors"
	"testing"

	"vozko/domain/workflow"
)

type conversationStartHarness struct {
	starter  *ConversationRunStarter
	wfRepo   *MockWorkflowRepository
	runRepo  *MockWorkflowRunRepository
	launcher *fakeLauncher
	state    *fakeSharedState
}

func tapWorkflow() *workflow.Workflow {
	return &workflow.Workflow{
		ID:          "wf-tap",
		WorkspaceID: "ws1",
		Status:      workflow.WorkflowStatusActive,
		Type:        workflow.WorkflowTypeMessages,
		Graph: workflow.Graph{
			Nodes: []workflow.Node{
				{ID: "tmr", Type: workflow.NodeTypeTriggerMessageReceived},
				{ID: "e1", Type: workflow.NodeTypeEnd},
			},
			Edges: []workflow.Edge{{Source: "tmr", Target: "e1"}},
		},
	}
}

func newConversationStartHarness() *conversationStartHarness {
	h := &conversationStartHarness{
		wfRepo:   NewMockWorkflowRepository(),
		runRepo:  NewMockWorkflowRunRepository(),
		launcher: &fakeLauncher{},
		state:    &fakeSharedState{allow: true},
	}
	h.wfRepo.workflows["wf-tap"] = tapWorkflow()
	starter, err := NewConversationRunStarter(h.wfRepo, h.runRepo, h.launcher, h.state)
	if err != nil {
		panic(err)
	}
	h.starter = starter
	return h
}

func tapStart() ConversationStart {
	data := map[string]interface{}{"message": "Quero o catálogo"}
	workflow.ApplyTriggerMessage(data, "row-42")
	workflow.ApplySelection(data, workflow.TemplateButtonSelection("Quero o catálogo", 1, true))
	return ConversationStart{WorkspaceID: "ws1", WorkflowID: "wf-tap", EntryID: "entry1", EntryType: "whatsapp", Data: data}
}

func TestStartForConversationLaunchesThatWorkflowOnceWithTheTapData(t *testing.T) {
	h := newConversationStartHarness()

	started, err := h.starter.StartForConversation(tapStart())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if started.RunID == "" || started.AlreadyRunning {
		t.Fatalf("expected a fresh run, got %+v", started)
	}
	if len(h.launcher.launched) != 1 {
		t.Fatalf("expected one launch, got %d", len(h.launcher.launched))
	}
	run := h.launcher.launched[0]
	if run.WorkflowID != "wf-tap" || run.WorkspaceID != "ws1" || run.EntryID != "entry1" || run.EntryType != "whatsapp" {
		t.Fatalf("run is not for the tapped conversation: %+v", run)
	}
	if run.TriggerNodeID != "tmr" || run.CurrentNodeID != "tmr" {
		t.Fatalf("run must start at the workflow trigger, got %s/%s", run.TriggerNodeID, run.CurrentNodeID)
	}
	if got := workflow.TriggerMessageID(&run.State); got != "row-42" {
		t.Fatalf("expected the trigger message row id, got %q", got)
	}
	if got := run.State.GetString("selected_option_id"); got != workflow.TemplateButtonOptionID(1) {
		t.Fatalf("expected the tapped button selection, got %q", got)
	}
}

func TestStartForConversationDoesNotStartASecondRunWhileOneIsActive(t *testing.T) {
	h := newConversationStartHarness()
	h.runRepo.runs["r-existing"] = &workflow.WorkflowRun{
		ID: "r-existing", WorkflowID: "wf-tap", EntryID: "entry1", TriggerNodeID: "tmr", Status: workflow.RunStatusWaiting,
	}

	started, err := h.starter.StartForConversation(tapStart())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !started.AlreadyRunning || started.RunID != "r-existing" {
		t.Fatalf("expected the active run, got %+v", started)
	}
	if len(h.launcher.launched) != 0 {
		t.Fatal("a second run must not launch")
	}
}

func TestStartForConversationRefusesWhatTheWorkflowRuleRefuses(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*workflow.Workflow)
		want   error
	}{
		{"another workspace", func(w *workflow.Workflow) { w.WorkspaceID = "ws-other" }, workflow.ErrWorkflowNotFound},
		{"paused", func(w *workflow.Workflow) { w.Status = workflow.WorkflowStatusPaused }, workflow.ErrWorkflowNotActive},
		{"voice", func(w *workflow.Workflow) { w.Type = workflow.WorkflowTypeVoice }, workflow.ErrInvalidWorkflowType},
		{"no trigger", func(w *workflow.Workflow) { w.Graph.Nodes = w.Graph.Nodes[1:] }, workflow.ErrGraphNoTrigger},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newConversationStartHarness()
			tc.mutate(h.wfRepo.workflows["wf-tap"])
			if _, err := h.starter.StartForConversation(tapStart()); !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
			if len(h.launcher.launched) != 0 || h.state.incrs != 0 {
				t.Fatal("a refused start must not take a slot or launch")
			}
		})
	}
}

func TestStartForConversationRefusesAnUnknownWorkflowAndPropagatesLookupErrors(t *testing.T) {
	h := newConversationStartHarness()
	start := tapStart()
	start.WorkflowID = "wf-missing"
	if _, err := h.starter.StartForConversation(start); !errors.Is(err, workflow.ErrWorkflowNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}

	h.wfRepo.FindErr = errors.New("db down")
	if _, err := h.starter.StartForConversation(tapStart()); err == nil || errors.Is(err, workflow.ErrWorkflowNotFound) {
		t.Fatalf("expected the lookup failure, got %v", err)
	}
}

func TestStartForConversationAtCapacityAndCreateFailureReleaseTheSlot(t *testing.T) {
	h := newConversationStartHarness()
	h.state.allow = false
	if _, err := h.starter.StartForConversation(tapStart()); !errors.Is(err, workflow.ErrWorkspaceAtCapacity) {
		t.Fatalf("expected capacity refusal, got %v", err)
	}

	h = newConversationStartHarness()
	h.runRepo.CreateErr = errors.New("insert failed")
	if _, err := h.starter.StartForConversation(tapStart()); err == nil {
		t.Fatal("expected the create failure")
	}
	if h.state.decrs != 1 || len(h.launcher.launched) != 0 {
		t.Fatalf("the slot must be released and nothing launched, decrs=%d launched=%d", h.state.decrs, len(h.launcher.launched))
	}
}

func TestNewConversationRunStarterRefusesMissingDependencies(t *testing.T) {
	if _, err := NewConversationRunStarter(nil, NewMockWorkflowRunRepository(), &fakeLauncher{}, &fakeSharedState{}); err == nil {
		t.Fatal("expected a refusal without the workflow repository")
	}
	if _, err := NewConversationRunStarter(NewMockWorkflowRepository(), nil, &fakeLauncher{}, &fakeSharedState{}); err == nil {
		t.Fatal("expected a refusal without the run repository")
	}
	if _, err := NewConversationRunStarter(NewMockWorkflowRepository(), NewMockWorkflowRunRepository(), nil, &fakeSharedState{}); err == nil {
		t.Fatal("expected a refusal without the launcher")
	}
}
