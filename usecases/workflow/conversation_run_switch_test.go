package workflow_usecase

import (
	"context"
	"testing"
	"vozko/domain/workflow"
)

type switchCanceller struct{ runs *MockWorkflowRunRepository }

func (c switchCanceller) CancelConversationRuns(_ context.Context, ws, entry, channel, reason string) ([]workflow.CancelledRun, error) {
	var cancelled []workflow.CancelledRun
	for _, run := range c.runs.runs {
		if run.WorkspaceID == ws && run.EntryID == entry && run.EntryType == channel && run.FollowsConversationOwner() && !run.Status.IsTerminal() {
			run.SetCancelled()
			run.Error = reason
			cancelled = append(cancelled, workflow.CancelledRun{RunID: run.ID, CurrentNodeID: run.CurrentNodeID})
		}
	}
	return cancelled, nil
}

func TestSwitchCancelsOldRunsAndStartsThePublishedWorkflowWithFreshState(t *testing.T) {
	h := newConversationStartHarness()
	h.wfRepo.publishLikeTheBootRepair(h.wfRepo.workflows["wf-tap"])
	old := &workflow.WorkflowRun{ID: "old", WorkspaceID: "ws1", WorkflowID: "previous", EntryID: "entry1", EntryType: "whatsapp", TriggerType: workflow.TriggerMessageReceived, Status: workflow.RunStatusWaiting, State: workflow.NewRunState(), CurrentNodeID: "old-wait"}
	old.State.Set("old_booking", "must not leak")
	if err := h.runRepo.Create(old); err != nil {
		t.Fatal(err)
	}
	webhook := &workflow.WorkflowRun{ID: "webhook", WorkspaceID: "ws1", WorkflowID: "independent", EntryID: "entry1", EntryType: "whatsapp", TriggerType: workflow.TriggerWebhook, Status: workflow.RunStatusWaiting, State: workflow.NewRunState()}
	if err := h.runRepo.Create(webhook); err != nil {
		t.Fatal(err)
	}
	changes := NewConversationRunSwitch(switchCanceller{h.runRepo}, h.starter, NewMockWorkflowRunLogRepository())
	if err := changes.StopConversationRuns(context.Background(), "ws1", "entry1", "whatsapp"); err != nil {
		t.Fatal(err)
	}
	if err := changes.StartConversationWorkflow(context.Background(), "ws1", "entry1", "whatsapp", "wf-tap", "user"); err != nil {
		t.Fatal(err)
	}
	cancelled, _ := h.runRepo.FindByID("old")
	if cancelled.Status != workflow.RunStatusCancelled {
		t.Fatal("old workflow was not stopped")
	}
	independent, _ := h.runRepo.FindByID("webhook")
	if independent.Status != workflow.RunStatusWaiting {
		t.Fatal("independent webhook was cancelled by a conversation handoff")
	}
	if len(h.launcher.launched) != 1 {
		t.Fatalf("launches=%d", len(h.launcher.launched))
	}
	fresh := h.launcher.launched[0]
	if fresh.CurrentNodeID != "tmr" || fresh.WorkflowVersion == nil || fresh.State.GetString("old_booking") != "" {
		t.Fatalf("new workflow was not started from scratch: %+v", fresh)
	}
}
