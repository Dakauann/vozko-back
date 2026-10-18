package workflow

import "strings"

func (w *Workflow) StartsConversationsIn(workspaceID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	switch {
	case w == nil || workspaceID == "" || w.WorkspaceID != workspaceID:
		return ErrWorkflowNotFound
	case w.Status != WorkflowStatusActive:
		return ErrWorkflowNotActive
	case w.Type == WorkflowTypeVoice:
		return ErrInvalidWorkflowType
	}
	return nil
}
