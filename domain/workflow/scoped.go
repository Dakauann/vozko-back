package workflow

import (
	"errors"

	wd "vozko/domain/workspace/workspace_department"
)

var ErrWorkflowAccessDenied = errors.New("workflow: no access to this workflow")

type Scope struct {
	WorkspaceID string
	Departments *wd.DepartmentFilter
}

type ScopedWorkflowsUseCase interface {
	Get(scope Scope, workflowID string) (*Workflow, error)
	Update(scope Scope, workflowID string, changes *Workflow) (*Workflow, error)
	Delete(scope Scope, workflowID string) error
	Activate(scope Scope, workflowID string) (*Workflow, error)
	Pause(scope Scope, workflowID string) (*Workflow, error)
}
