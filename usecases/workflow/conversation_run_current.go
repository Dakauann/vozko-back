package workflow_usecase

import (
	"fmt"
	"vozko/domain/workflow"
)

const cancelReasonPublishedVersionChanged = "published_workflow_version_changed"

func conversationRunCurrent(runs workflow.WorkflowRunRepository, engine *RunEngine, run *workflow.WorkflowRun, published *workflow.Workflow, event workflow.TriggerEvent) (*workflow.WorkflowRun, error) {
	if run == nil || !run.FollowsConversationOwner() {
		return run, nil
	}
	if run.WorkspaceID != event.WorkspaceID || run.EntryType != event.EntryType {
		return nil, fmt.Errorf("workflow: waiting run does not belong to the incoming conversation")
	}
	reason := ""
	expected, _ := event.Data[workflow.DataKeyCampaignWorkflowID].(string)
	if expected == "" {
		expected, _ = event.Data[workflow.DataKeyAccountWorkflowID].(string)
	}
	if expected != "" && expected != run.WorkflowID {
		reason = cancelReasonAutomationChanged
	}
	if published != nil && run.WorkflowVersion != nil && published.PublishedVersion != nil && *run.WorkflowVersion != *published.PublishedVersion {
		reason = cancelReasonPublishedVersionChanged
	}
	if reason == "" {
		return run, nil
	}
	run.SetCancelled()
	run.Error = reason
	if err := engine.saveWith(runs, run); err != nil {
		return nil, err
	}
	traceCancelledStep(engine.logRepo, run.ID, run.CurrentNodeID, reason, runLogAttrs(run, run.CurrentNodeID))
	return nil, nil
}
