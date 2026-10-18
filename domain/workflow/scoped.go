package workflow

import (
	"context"
	"errors"

	"vozko/domain/workspace"
	wd "vozko/domain/workspace/workspace_department"
)

var ErrWorkflowAccessDenied = errors.New("workflow: no access to this workflow")

type Scope struct {
	WorkspaceID string
	Departments *wd.DepartmentFilter
	Requester   workspace.Requester
}

type ScopedWorkflowsUseCase interface {
	Create(ctx context.Context, scope Scope, w *Workflow) (*Workflow, error)
	Get(scope Scope, workflowID string) (*Workflow, error)
	Update(ctx context.Context, scope Scope, workflowID string, changes *Workflow) (*Workflow, error)
	Delete(scope Scope, workflowID string) error
	Activate(ctx context.Context, scope Scope, workflowID string) (*Workflow, error)
	Pause(scope Scope, workflowID string) (*Workflow, error)
}
