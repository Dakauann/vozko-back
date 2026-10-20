package automationrefs

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/pipeline"
	"vozko/domain/workflow"
	wd "vozko/domain/workspace/workspace_department"
)

type agents map[string]*agent.Agent

func (a agents) FindByID(id string) (*agent.Agent, error) {
	if found, ok := a[id]; ok {
		return found, nil
	}
	return nil, errors.New("not found")
}

type workflows map[string]*workflow.Workflow

func (w workflows) FindByID(id string) (*workflow.Workflow, error) {
	if found, ok := w[id]; ok {
		return found, nil
	}
	return nil, errors.New("not found")
}

type pipelines map[string]*pipeline.Pipeline

func (p pipelines) GetByID(_, id string) (*pipeline.Pipeline, error) {
	if found, ok := p[id]; ok {
		return found, nil
	}
	return nil, errors.New("not found")
}

type departments map[string]*wd.Department

func (d departments) GetDepartmentByID(id string) (*wd.Department, error) {
	if found, ok := d[id]; ok {
		return found, nil
	}
	return nil, errors.New("not found")
}

func ptr(s string) *string { return &s }

func finders() Finders {
	return Finders{
		Agents:      agents{"ag-1": {WorkspaceID: "ws-1"}, "ag-x": {WorkspaceID: "ws-2"}},
		Workflows:   workflows{"wf-1": {WorkspaceID: "ws-1"}},
		Pipelines:   pipelines{"p-1": {WorkspaceID: "ws-1", ObjectType: pipeline.ObjectConversation}, "p-deal": {WorkspaceID: "ws-1", ObjectType: "deal"}},
		Departments: departments{"d-1": {WorkspaceID: "ws-1"}},
	}
}

func TestReferencesMustLiveInTheWorkspace(t *testing.T) {
	cases := []struct {
		name string
		refs Refs
		ok   bool
	}{
		{"nothing set", Refs{}, true},
		{"all in the workspace", Refs{AgentID: ptr("ag-1"), WorkflowID: ptr("wf-1"), PipelineID: ptr("p-1"), DepartmentID: ptr("d-1")}, true},
		{"agent of another workspace", Refs{AgentID: ptr("ag-x")}, false},
		{"unknown workflow", Refs{WorkflowID: ptr("wf-9")}, false},
		{"funnel that is not a conversation funnel", Refs{PipelineID: ptr("p-deal")}, false},
		{"unknown department", Refs{DepartmentID: ptr("d-9")}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := finders().OwnedBy(context.Background(), "ws-1", tc.refs)
			if tc.ok && err != nil {
				t.Fatalf("err = %v", err)
			}
			if !tc.ok && !errors.Is(err, ErrNotInWorkspace) {
				t.Fatalf("err = %v, want ErrNotInWorkspace", err)
			}
		})
	}
}

func TestMissingFindersRefuseInsteadOfAllowing(t *testing.T) {
	if err := (Finders{}).OwnedBy(context.Background(), "ws-1", Refs{AgentID: ptr("ag-1")}); !errors.Is(err, ErrNotInWorkspace) {
		t.Fatalf("err = %v", err)
	}
}
