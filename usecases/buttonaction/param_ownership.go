package buttonaction_usecase

import (
	"context"
	"fmt"

	"vozko/domain/buttonaction"
	"vozko/domain/calls/calllist"
	"vozko/domain/conversation"
	"vozko/domain/customfield"
	"vozko/domain/pipeline"
	"vozko/domain/stage"
	"vozko/domain/workflow"
	"vozko/domain/workspace"
	wd "vozko/domain/workspace/workspace_department"
)

type OwnershipDeps struct {
	Workflows workflow.PublishedReader
	CallLists interface {
		Get(ctx context.Context, workspaceID, id string) (*calllist.List, error)
	}
	Fields interface {
		ListByObject(workspaceID string, objectType customfield.ObjectType) ([]*customfield.Definition, error)
	}
	Pipelines interface {
		GetByID(workspaceID, id string) (*pipeline.Pipeline, error)
	}
	Stages interface {
		FindByID(id string) (*stage.Stage, error)
	}
	Departments interface {
		GetDepartmentByID(id string) (*wd.Department, error)
	}
	Members interface {
		GetMember(workspaceID, userID string) (*workspace.Member, error)
	}
	Automations interface {
		Usable(workspaceID string, a conversation.Automation) error
	}
}

type WorkspaceOwnership struct {
	deps OwnershipDeps
}

var _ ParamOwnership = (*WorkspaceOwnership)(nil)

func NewWorkspaceOwnership(deps OwnershipDeps) (*WorkspaceOwnership, error) {
	if deps.Workflows == nil || deps.CallLists == nil || deps.Fields == nil || deps.Pipelines == nil || deps.Stages == nil ||
		deps.Departments == nil || deps.Members == nil || deps.Automations == nil {
		return nil, buttonaction.ErrUnavailable
	}
	return &WorkspaceOwnership{deps: deps}, nil
}

func (o *WorkspaceOwnership) Check(ctx context.Context, workspaceID string, kind buttonaction.Kind, p buttonaction.Params) error {
	owned, err := o.owns(ctx, workspaceID, kind, p)
	if err != nil {
		return fmt.Errorf("check %s parameters: %w", kind, err)
	}
	if !owned {
		return buttonaction.ErrParamsNotInWorkspace
	}
	if kind == buttonaction.KindStartWorkflow {
		return o.startsFromAMessage(p.WorkflowID)
	}
	return nil
}

func (o *WorkspaceOwnership) startsFromAMessage(workflowID string) error {
	running, err := workflow.LoadPublished(o.deps.Workflows, workflowID)
	if err != nil {
		return fmt.Errorf("read the version of workflow %s that runs: %w", workflowID, err)
	}
	if running == nil {
		return buttonaction.ErrParamsNotInWorkspace
	}
	_, err = workflow.MessageStartTrigger(running.Graph.Nodes)
	return err
}

func (o *WorkspaceOwnership) owns(ctx context.Context, workspaceID string, kind buttonaction.Kind, p buttonaction.Params) (bool, error) {
	switch kind {
	case buttonaction.KindOptOut, buttonaction.KindConsentGrant:
		return true, nil
	case buttonaction.KindHandOff:
		return o.handOffTarget(workspaceID, p), nil
	case buttonaction.KindDelegate:
		return o.deps.Automations.Usable(workspaceID, p.Automation()) == nil, nil
	case buttonaction.KindStartWorkflow:
		w, err := o.deps.Workflows.FindByID(p.WorkflowID)
		return err == nil && w != nil && w.WorkspaceID == workspaceID, nil
	case buttonaction.KindCallList:
		l, err := o.deps.CallLists.Get(ctx, workspaceID, p.CallListID)
		return err == nil && l != nil, nil
	case buttonaction.KindClassify:
		return o.selectOption(workspaceID, p)
	case buttonaction.KindCreateDeal:
		return o.pipelineStage(workspaceID, p), nil
	}
	return false, buttonaction.ErrKindUnknown
}

func (o *WorkspaceOwnership) handOffTarget(workspaceID string, p buttonaction.Params) bool {
	if p.DepartmentID != "" {
		d, err := o.deps.Departments.GetDepartmentByID(p.DepartmentID)
		return err == nil && d != nil && d.WorkspaceID == workspaceID
	}
	m, err := o.deps.Members.GetMember(workspaceID, p.UserID)
	return err == nil && m != nil && m.WorkspaceID == workspaceID
}

func (o *WorkspaceOwnership) selectOption(workspaceID string, p buttonaction.Params) (bool, error) {
	defs, err := o.deps.Fields.ListByObject(workspaceID, customfield.ObjectLead)
	if err != nil {
		return false, err
	}
	for _, d := range defs {
		if d != nil && d.Key == p.FieldKey && d.Type == customfield.TypeSelect {
			return d.ValidateValue(p.Option) == nil, nil
		}
	}
	return false, nil
}

func (o *WorkspaceOwnership) pipelineStage(workspaceID string, p buttonaction.Params) bool {
	found, err := o.deps.Pipelines.GetByID(workspaceID, p.PipelineID)
	if err != nil || found == nil || found.WorkspaceID != workspaceID {
		return false
	}
	st, err := o.deps.Stages.FindByID(p.StageID)
	return err == nil && st != nil && st.WorkspaceID == workspaceID && st.PipelineID == p.PipelineID
}
