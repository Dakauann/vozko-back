package automationrefs

import (
	"context"
	"errors"

	"vozko/domain/agent"
	"vozko/domain/pipeline"
	"vozko/domain/workflow"
	wd "vozko/domain/workspace/workspace_department"
)

var ErrNotInWorkspace = errors.New("the selected agent, workflow, funnel or department is not in this workspace")

type AgentFinder interface {
	FindByID(agentID string) (*agent.Agent, error)
}

type WorkflowFinder interface {
	FindByID(workflowID string) (*workflow.Workflow, error)
}

type PipelineFinder interface {
	GetByID(workspaceID, id string) (*pipeline.Pipeline, error)
}

type DepartmentFinder interface {
	GetDepartmentByID(id string) (*wd.Department, error)
}

type Finders struct {
	Agents      AgentFinder
	Workflows   WorkflowFinder
	Pipelines   PipelineFinder
	Departments DepartmentFinder
}

type Refs struct {
	AgentID      *string
	WorkflowID   *string
	PipelineID   *string
	DepartmentID *string
}

func (f Finders) OwnedBy(_ context.Context, workspaceID string, refs Refs) error {
	if refs.AgentID != nil {
		if f.Agents == nil {
			return ErrNotInWorkspace
		}
		a, err := f.Agents.FindByID(*refs.AgentID)
		if err != nil || a == nil || a.WorkspaceID != workspaceID {
			return ErrNotInWorkspace
		}
	}
	if refs.WorkflowID != nil {
		if f.Workflows == nil {
			return ErrNotInWorkspace
		}
		wf, err := f.Workflows.FindByID(*refs.WorkflowID)
		if err != nil || wf == nil || wf.WorkspaceID != workspaceID {
			return ErrNotInWorkspace
		}
	}
	if refs.PipelineID != nil {
		if f.Pipelines == nil {
			return ErrNotInWorkspace
		}
		p, err := f.Pipelines.GetByID(workspaceID, *refs.PipelineID)
		if err != nil || p == nil || p.WorkspaceID != workspaceID || p.ObjectType != pipeline.ObjectConversation {
			return ErrNotInWorkspace
		}
	}
	if refs.DepartmentID != nil {
		if f.Departments == nil {
			return ErrNotInWorkspace
		}
		d, err := f.Departments.GetDepartmentByID(*refs.DepartmentID)
		if err != nil || d == nil || d.WorkspaceID != workspaceID {
			return ErrNotInWorkspace
		}
	}
	return nil
}
