package workflow

import (
	"fmt"
	"strings"
)

func (w *Workflow) StartsConversationsIn(workspaceID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	if w == nil || workspaceID == "" || w.WorkspaceID != workspaceID {
		return ErrWorkflowNotFound
	}
	switch WorkflowBlockReason(w) {
	case EntryBlockedNotPublished:
		return fmt.Errorf("%w: workflow %s", ErrWorkflowNotPublished, w.ID)
	case EntryBlockedPaused:
		return ErrWorkflowNotActive
	}
	if w.Type == WorkflowTypeVoice {
		return ErrInvalidWorkflowType
	}
	return nil
}

func (w *Workflow) CallableByAgentIn(workspaceID string) ([]AgentCallInput, error) {
	if err := w.StartsConversationsIn(workspaceID); err != nil {
		return nil, err
	}
	if w.Graph.TriggerNodeByType(TriggerAgentCall) == nil {
		return nil, fmt.Errorf("%w: %s", ErrTriggerNotInWorkflow, TriggerAgentCall)
	}
	return w.Graph.AgentCallInputs()
}
