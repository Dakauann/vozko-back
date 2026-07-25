package workflow

import "vozko/domain/shared"

type ListWorkflowsInput struct {
	WorkspaceID   string
	DepartmentIDs []string
	Search        string
	Status        *WorkflowStatus

	Type *WorkflowType

	TriggerType *TriggerType
	Options     shared.QueryOptions
}

type ListRunsInput struct {
	WorkspaceID string
	WorkflowID  string
	EntryID     string
	Status      *RunStatus
	Options     shared.QueryOptions
}
