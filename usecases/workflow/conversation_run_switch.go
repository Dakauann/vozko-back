package workflow_usecase

import (
	"context"
	"fmt"
	"vozko/domain/workflow"
)

const cancelReasonAutomationChanged = "conversation_automation_changed"

type ConversationRunSwitch struct {
	cancels workflow.ConversationRunCanceller
	starter conversationStarter
	logs    workflow.WorkflowRunLogRepository
}

func NewConversationRunSwitch(cancels workflow.ConversationRunCanceller, starter *ConversationRunStarter, logs workflow.WorkflowRunLogRepository) *ConversationRunSwitch {
	if cancels == nil || starter == nil || logs == nil {
		panic("workflow: conversation switching requires cancellation, a starter and execution logs")
	}
	return &ConversationRunSwitch{cancels: cancels, starter: starter, logs: logs}
}

func (s *ConversationRunSwitch) StopConversationRuns(ctx context.Context, workspaceID, entryID, entryType string) error {
	cancelled, err := s.cancels.CancelConversationRuns(ctx, workspaceID, entryID, entryType, cancelReasonAutomationChanged)
	if err != nil {
		return fmt.Errorf("stop the previous conversation workflow: %w", err)
	}
	for _, run := range cancelled {
		traceCancelledStep(s.logs, run.RunID, run.CurrentNodeID, cancelReasonAutomationChanged, nil)
	}
	return nil
}

func (s *ConversationRunSwitch) StartConversationWorkflow(_ context.Context, workspaceID, entryID, entryType, workflowID, by string) error {
	_, err := s.starter.StartForConversation(ConversationStart{WorkspaceID: workspaceID, WorkflowID: workflowID, EntryID: entryID, EntryType: entryType, StartedBy: workflow.StartedByManual(by), Data: workflow.MessageStartRunData(workflow.MessageStart{Channel: entryType, WorkspaceID: workspaceID})})
	return err
}
