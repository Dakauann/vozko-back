package workflow_usecase

import "vozko/domain/workflow"

type scopedWorkflows struct {
	get      workflow.GetWorkflowUseCase
	update   workflow.UpdateWorkflowUseCase
	delete   workflow.DeleteWorkflowUseCase
	activate workflow.ActivateWorkflowUseCase
	pause    workflow.PauseWorkflowUseCase
}

func NewScopedWorkflowsUseCase(
	get workflow.GetWorkflowUseCase,
	update workflow.UpdateWorkflowUseCase,
	del workflow.DeleteWorkflowUseCase,
	activate workflow.ActivateWorkflowUseCase,
	pause workflow.PauseWorkflowUseCase,
) workflow.ScopedWorkflowsUseCase {
	return &scopedWorkflows{get: get, update: update, delete: del, activate: activate, pause: pause}
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

func (uc *scopedWorkflows) Update(scope workflow.Scope, workflowID string, changes *workflow.Workflow) (*workflow.Workflow, error) {
	if _, err := uc.Get(scope, workflowID); err != nil {
		return nil, err
	}
	return uc.update.Execute(workflowID, changes)
}

func (uc *scopedWorkflows) Delete(scope workflow.Scope, workflowID string) error {
	if _, err := uc.Get(scope, workflowID); err != nil {
		return err
	}
	return uc.delete.Execute(workflowID)
}

func (uc *scopedWorkflows) Activate(scope workflow.Scope, workflowID string) (*workflow.Workflow, error) {
	if _, err := uc.Get(scope, workflowID); err != nil {
		return nil, err
	}
	return uc.activate.Execute(workflowID)
}

func (uc *scopedWorkflows) Pause(scope workflow.Scope, workflowID string) (*workflow.Workflow, error) {
	if _, err := uc.Get(scope, workflowID); err != nil {
		return nil, err
	}
	return uc.pause.Execute(workflowID)
}
