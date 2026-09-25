package copilottools

import (
	"context"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/workflow"
)

const knownWorkflow = "8e7d6c5b-4a39-4281-9f0e-1d2c3b4a5f6e"

type fakeScopedWorkflows struct {
	scope workflow.Scope
	ops   []string
	err   error
}

func (f *fakeScopedWorkflows) Get(scope workflow.Scope, id string) (*workflow.Workflow, error) {
	f.scope = scope
	if f.err != nil {
		return nil, f.err
	}
	return &workflow.Workflow{ID: id, Name: "Boas-vindas", Status: "active"}, nil
}

func (f *fakeScopedWorkflows) Update(workflow.Scope, string, *workflow.Workflow) (*workflow.Workflow, error) {
	return nil, nil
}

func (f *fakeScopedWorkflows) Delete(workflow.Scope, string) error { return nil }

func (f *fakeScopedWorkflows) Activate(scope workflow.Scope, id string) (*workflow.Workflow, error) {
	return f.change("activate", scope, id)
}

func (f *fakeScopedWorkflows) Pause(scope workflow.Scope, id string) (*workflow.Workflow, error) {
	return f.change("pause", scope, id)
}

func (f *fakeScopedWorkflows) change(op string, scope workflow.Scope, id string) (*workflow.Workflow, error) {
	f.scope = scope
	f.ops = append(f.ops, op+":"+id)
	if f.err != nil {
		return nil, f.err
	}
	return &workflow.Workflow{ID: id, Name: "Boas-vindas", Status: "paused"}, nil
}

func TestWorkflowStatusChangesNeedApprovalAndWorkflowUpdate(t *testing.T) {
	wfs := &fakeScopedWorkflows{}
	for _, tool := range []copilot.Tool{NewPauseWorkflowTool(wfs), NewActivateWorkflowTool(wfs)} {
		if m := tool.Meta(); !m.Mutating || m.Resource != "workflows" || m.Action != "update" {
			t.Fatalf("%s meta = %+v", tool.Definition().Name, m)
		}
	}
}

func TestPauseWorkflowChangesItInsideTheUsersScope(t *testing.T) {
	wfs := &fakeScopedWorkflows{}
	cc := member()
	res := NewPauseWorkflowTool(wfs).Execute(context.Background(), cc, map[string]interface{}{"workflow_id": knownWorkflow})
	if res.Status != copilot.StatusOK || len(wfs.ops) != 1 || wfs.ops[0] != "pause:"+knownWorkflow {
		t.Fatalf("status %s ops %v", res.Status, wfs.ops)
	}
	if wfs.scope.WorkspaceID != "ws-1" || wfs.scope.Departments != cc.Departments {
		t.Fatalf("scope = %+v", wfs.scope)
	}
}

func TestWorkflowToolsReportOutOfScopeAsDenied(t *testing.T) {
	for err, want := range map[error]copilot.Status{
		workflow.ErrWorkflowAccessDenied: copilot.StatusDenied,
		workflow.ErrWorkflowNotFound:     copilot.StatusError,
	} {
		res := NewActivateWorkflowTool(&fakeScopedWorkflows{err: err}).Execute(context.Background(), member(), map[string]interface{}{"workflow_id": knownWorkflow})
		if res.Status != want {
			t.Fatalf("%v: status %s", err, res.Status)
		}
	}
}

func TestWorkflowToolsDescribeTheWorkflowByName(t *testing.T) {
	fields := NewPauseWorkflowTool(&fakeScopedWorkflows{}).(copilot.Describer).Describe(context.Background(), member(), map[string]interface{}{"workflow_id": knownWorkflow})
	if len(fields) != 1 || fields[0].Key != "workflow" || fields[0].Value != "Boas-vindas" {
		t.Fatalf("fields = %v", fields)
	}
}

func TestWorkflowToolsRefuseAnInventedId(t *testing.T) {
	wfs := &fakeScopedWorkflows{}
	if res := NewPauseWorkflowTool(wfs).Execute(context.Background(), member(), map[string]interface{}{"workflow_id": "boas-vindas"}); res.Status != copilot.StatusError || len(wfs.ops) != 0 {
		t.Fatalf("status %s ops %v", res.Status, wfs.ops)
	}
}
