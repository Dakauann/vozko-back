package workflow_usecase

import (
	"context"

	"vozko/domain/workflow"
	"vozko/domain/workspace"
)

type ScopedWorkflowsDeps struct {
	Create      workflow.CreateWorkflowUseCase
	Get         workflow.GetWorkflowUseCase
	Update      workflow.UpdateWorkflowUseCase
	Delete      workflow.DeleteWorkflowUseCase
	Activate    workflow.ActivateWorkflowUseCase
	Pause       workflow.PauseWorkflowUseCase
	Permissions workspace.PermissionChecker
}

type scopedWorkflows struct {
	create      workflow.CreateWorkflowUseCase
	get         workflow.GetWorkflowUseCase
	update      workflow.UpdateWorkflowUseCase
	delete      workflow.DeleteWorkflowUseCase
	activate    workflow.ActivateWorkflowUseCase
	pause       workflow.PauseWorkflowUseCase
	permissions workspace.PermissionChecker
}

func NewScopedWorkflowsUseCase(deps ScopedWorkflowsDeps) workflow.ScopedWorkflowsUseCase {
	return &scopedWorkflows{
		create: deps.Create, get: deps.Get, update: deps.Update, delete: deps.Delete,
		activate: deps.Activate, pause: deps.Pause, permissions: deps.Permissions,
	}
}

func (uc *scopedWorkflows) Create(ctx context.Context, scope workflow.Scope, w *workflow.Workflow) (*workflow.Workflow, error) {
	if w != nil && workflow.GrantsConsent(&w.Graph) {
		if err := uc.requireConsentGrant(scope); err != nil {
			return nil, err
		}
	}
	return uc.create.Execute(ctx, w)
}

func (uc *scopedWorkflows) Get(scope workflow.Scope, workflowID string) (*workflow.Workflow, error) {
	wf, err := uc.get.Execute(workflowID)
	if err != nil {
		return nil, err
	}
	if wf == nil || scope.WorkspaceID == "" || wf.WorkspaceID != scope.WorkspaceID {
		return nil, workflow.ErrWorkflowNotFound
	}
	if !scope.Departments.Allows(wf.DepartmentID) {
		return nil, workflow.ErrWorkflowAccessDenied
	}
	return wf, nil
}

func (uc *scopedWorkflows) Update(ctx context.Context, scope workflow.Scope, workflowID string, changes *workflow.Workflow) (*workflow.Workflow, error) {
	stored, err := uc.Get(scope, workflowID)
	if err != nil {
		return nil, err
	}
	if changes != nil && len(workflow.ChangedConsentGrants(&stored.Graph, &changes.Graph)) > 0 {
		if err := uc.requireConsentGrant(scope); err != nil {
			return nil, err
		}
	}
	return uc.update.Execute(ctx, workflowID, changes)
}

func (uc *scopedWorkflows) Delete(scope workflow.Scope, workflowID string) error {
	if _, err := uc.Get(scope, workflowID); err != nil {
		return err
	}
	return uc.delete.Execute(workflowID)
}

func (uc *scopedWorkflows) Activate(ctx context.Context, scope workflow.Scope, workflowID string) (*workflow.Workflow, error) {
	stored, err := uc.Get(scope, workflowID)
	if err != nil {
		return nil, err
	}
	if workflow.GrantsConsent(&stored.Graph) {
		if err := uc.requireConsentGrant(scope); err != nil {
			return nil, err
		}
	}
	return uc.activate.Execute(ctx, workflowID)
}

func (uc *scopedWorkflows) Pause(scope workflow.Scope, workflowID string) (*workflow.Workflow, error) {
	if _, err := uc.Get(scope, workflowID); err != nil {
		return nil, err
	}
	return uc.pause.Execute(workflowID)
}

func (uc *scopedWorkflows) requireConsentGrant(scope workflow.Scope) error {
	return workspace.RequireConsentGrant(uc.permissions, scope.WorkspaceID, scope.Requester)
}
