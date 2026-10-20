package workflow

import (
	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type ListWorkflowsInput struct {
	WorkspaceID   string
	DepartmentIDs []string
	Search        string
	Status        *WorkflowStatus

	Type *WorkflowType

	TriggerType *TriggerType
	EntryKind   *EntryPointKind
	Options     shared.QueryOptions
}

type ListRunsInput struct {
	WorkspaceID string
	WorkflowID  string
	EntryID     string
	Status      *RunStatus
	Visibility  conversation.VisibilityScope
	Options     shared.QueryOptions
}
