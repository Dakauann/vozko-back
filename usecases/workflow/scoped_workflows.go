package workflow_usecase

import (
	"context"
	"errors"

	"vozko/domain/shared"
	"vozko/domain/workflow"
	"vozko/domain/workspace"
)

type ScopedWorkflowsDeps struct {
	Create           workflow.CreateWorkflowUseCase
	Get              workflow.GetWorkflowUseCase
	Update           workflow.UpdateWorkflowUseCase
	Delete           workflow.DeleteWorkflowUseCase
	AssignDepartment workflow.AssignDepartmentUseCase
	Activate         workflow.ActivateWorkflowUseCase
	Publish          workflow.PublishWorkflowUseCase
	Pause            workflow.PauseWorkflowUseCase
	Versions         workflow.WorkflowVersionsUseCase
	Permissions      workspace.PermissionChecker
	PinSources       PinSources
	EntryPoints      EntryPointAttacher
}

type scopedWorkflows struct {
	create           workflow.CreateWorkflowUseCase
	get              workflow.GetWorkflowUseCase
	update           workflow.UpdateWorkflowUseCase
	delete           workflow.DeleteWorkflowUseCase
	assignDepartment workflow.AssignDepartmentUseCase
	activate         workflow.ActivateWorkflowUseCase
	publish          workflow.PublishWorkflowUseCase
	pause            workflow.PauseWorkflowUseCase
	versions         workflow.WorkflowVersionsUseCase
	permissions      workspace.PermissionChecker
	pinSources       PinSources
	entryPoints      EntryPointAttacher
}

var (
	errScopedVersionsMissing   = errors.New("workflow: publishing and versions are not wired")
	errScopedDepartmentMissing = errors.New("workflow: department assignment is not wired")
)

func NewScopedWorkflowsUseCase(deps ScopedWorkflowsDeps) workflow.ScopedWorkflowsUseCase {
	return &scopedWorkflows{
		create: deps.Create, get: deps.Get, update: deps.Update, delete: deps.Delete, assignDepartment: deps.AssignDepartment,
		activate: deps.Activate, publish: deps.Publish, pause: deps.Pause, versions: deps.Versions, permissions: deps.Permissions,
		pinSources: deps.PinSources, entryPoints: deps.EntryPoints,
	}
}

func (uc *scopedWorkflows) shown(scope workflow.Scope, wf *workflow.Workflow, err error) (*workflow.Workflow, error) {
	if err != nil {
		return nil, err
	}
	return withheldFor(uc.pinSources, viewerOf(scope), wf), nil
}

func (uc *scopedWorkflows) Create(ctx context.Context, scope workflow.Scope, w *workflow.Workflow) (*workflow.Workflow, error) {
	if w != nil && workflow.GrantsConsent(&w.Graph) {
		if err := uc.requireConsentGrant(scope); err != nil {
			return nil, err
		}
	}
	created, err := uc.create.Execute(ctx, w)
	return uc.shown(scope, created, err)
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

func (uc *scopedWorkflows) Detail(scope workflow.Scope, workflowID string) (*workflow.Workflow, error) {
	wf, err := uc.Get(scope, workflowID)
	if err != nil {
		return nil, err
	}
	published, err := uc.publishedVersion(wf)
	if err != nil {
		return nil, err
	}
	wf.RequiredCampaignVars = wf.RequiredCampaignVarsOf(published)
	if uc.entryPoints != nil {
		if err := uc.entryPoints.Attach(scope.WorkspaceID, []*workflow.Workflow{wf}); err != nil {
			return nil, err
		}
	}
	return uc.shown(scope, wf, nil)
}

func (uc *scopedWorkflows) Update(ctx context.Context, scope workflow.Scope, workflowID string, changes *workflow.Workflow, revision int) (*workflow.Workflow, error) {
	stored, err := uc.Get(scope, workflowID)
	if err != nil {
		return nil, err
	}
	if changes != nil && len(workflow.ChangedConsentGrants(&stored.Graph, &changes.Graph)) > 0 {
		if err := uc.requireConsentGrant(scope); err != nil {
			return nil, err
		}
	}
	updated, err := uc.update.Execute(ctx, workflowID, changes, revision)
	return uc.shown(scope, updated, err)
}

func (uc *scopedWorkflows) Delete(scope workflow.Scope, workflowID string) error {
	if _, err := uc.Get(scope, workflowID); err != nil {
		return err
	}
	return uc.delete.Execute(workflowID)
}

func (uc *scopedWorkflows) AssignDepartment(ctx context.Context, scope workflow.Scope, workflowID string) (*workflow.Workflow, error) {
	if _, err := uc.Get(scope, workflowID); err != nil {
		return nil, err
	}
	if uc.assignDepartment == nil {
		return nil, errScopedDepartmentMissing
	}
	assigned, err := uc.assignDepartment.Execute(ctx, workflowID)
	return uc.shown(scope, assigned, err)
}

func (uc *scopedWorkflows) Activate(ctx context.Context, scope workflow.Scope, workflowID string) (*workflow.Workflow, error) {
	stored, err := uc.Get(scope, workflowID)
	if err != nil {
		return nil, err
	}
	running, err := uc.graphToRun(stored)
	if err != nil {
		return nil, err
	}
	if workflow.GrantsConsent(running) {
		if err := uc.requireConsentGrant(scope); err != nil {
			return nil, err
		}
	}
	activated, err := uc.activate.Execute(workflow.WithPublisher(ctx, scope.Requester.UserID), workflowID)
	return uc.shown(scope, activated, err)
}

func (uc *scopedWorkflows) graphToRun(stored *workflow.Workflow) (*workflow.Graph, error) {
	published, err := uc.publishedVersion(stored)
	if err != nil || published == nil {
		return &stored.Graph, err
	}
	return published.Graph, nil
}

func (uc *scopedWorkflows) publishedVersion(w *workflow.Workflow) (*workflow.WorkflowVersion, error) {
	if w.PublishedVersion == nil {
		return nil, nil
	}
	if uc.versions == nil {
		return nil, errScopedVersionsMissing
	}
	return uc.versions.Find(w.ID, *w.PublishedVersion)
}

func (uc *scopedWorkflows) Publish(ctx context.Context, scope workflow.Scope, workflowID string) (*workflow.Workflow, error) {
	stored, err := uc.Get(scope, workflowID)
	if err != nil {
		return nil, err
	}
	if uc.publish == nil {
		return nil, errScopedVersionsMissing
	}
	if workflow.GrantsConsent(&stored.Graph) {
		if err := uc.requireConsentGrant(scope); err != nil {
			return nil, err
		}
	}
	published, err := uc.publish.Publish(workflow.WithPublisher(ctx, scope.Requester.UserID), workflowID)
	return uc.shown(scope, published, err)
}

func (uc *scopedWorkflows) Versions(scope workflow.Scope, workflowID string, options shared.QueryOptions) (*shared.PaginatedResult[*workflow.WorkflowVersion], error) {
	if _, err := uc.Get(scope, workflowID); err != nil {
		return nil, err
	}
	if uc.versions == nil {
		return nil, errScopedVersionsMissing
	}
	return uc.versions.List(workflowID, options)
}

func (uc *scopedWorkflows) Version(scope workflow.Scope, workflowID string, version int) (*workflow.WorkflowVersion, error) {
	if _, err := uc.Get(scope, workflowID); err != nil {
		return nil, err
	}
	if uc.versions == nil {
		return nil, errScopedVersionsMissing
	}
	return uc.versions.Find(workflowID, version)
}

func (uc *scopedWorkflows) RestoreVersion(ctx context.Context, scope workflow.Scope, workflowID string, version, revision int) (*workflow.Workflow, error) {
	stored, err := uc.Get(scope, workflowID)
	if err != nil {
		return nil, err
	}
	if uc.versions == nil {
		return nil, errScopedVersionsMissing
	}
	v, err := uc.versions.Find(workflowID, version)
	if err != nil {
		return nil, err
	}
	return uc.Update(ctx, scope, workflowID, &workflow.Workflow{
		Name:        stored.Name,
		Description: stored.Description,
		Type:        v.Type,
		Graph:       *v.Graph,
	}, revision)
}

func (uc *scopedWorkflows) Pause(scope workflow.Scope, workflowID string) (*workflow.Workflow, error) {
	if _, err := uc.Get(scope, workflowID); err != nil {
		return nil, err
	}
	paused, err := uc.pause.Execute(workflowID)
	return uc.shown(scope, paused, err)
}

func (uc *scopedWorkflows) requireConsentGrant(scope workflow.Scope) error {
	return workspace.RequireConsentGrant(uc.permissions, scope.WorkspaceID, scope.Requester)
}
