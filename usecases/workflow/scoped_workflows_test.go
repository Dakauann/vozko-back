package workflow_usecase

import (
	"errors"
	"testing"

	"vozko/domain/workflow"
	wd "vozko/domain/workspace/workspace_department"
)

type workflowCalls struct{ calls []string }

func (c *workflowCalls) record(op, id string) { c.calls = append(c.calls, op+":"+id) }

type getWorkflowStub map[string]*workflow.Workflow

func (g getWorkflowStub) Execute(id string) (*workflow.Workflow, error) {
	wf, ok := g[id]
	if !ok {
		return nil, workflow.ErrWorkflowNotFound
	}
	return wf, nil
}

type updateWorkflowStub struct{ *workflowCalls }

func (u updateWorkflowStub) Execute(id string, wf *workflow.Workflow) (*workflow.Workflow, error) {
	u.record("update", id)
	return wf, nil
}

type deleteWorkflowStub struct{ *workflowCalls }

func (d deleteWorkflowStub) Execute(id string) error {
	d.record("delete", id)
	return nil
}

type statusWorkflowStub struct {
	*workflowCalls
	op string
}

func (s statusWorkflowStub) Execute(id string) (*workflow.Workflow, error) {
	s.record(s.op, id)
	return &workflow.Workflow{ID: id}, nil
}

var workflowsByID = getWorkflowStub{
	"w-sales": {ID: "w-sales", WorkspaceID: "ws1", DepartmentID: "d-sales"},
	"w-other": {ID: "w-other", WorkspaceID: "ws2", DepartmentID: "d-sales"},
	"w-team":  {ID: "w-team", WorkspaceID: "ws1", DepartmentID: "d-support"},
}

func scopedFixture() (workflow.ScopedWorkflowsUseCase, *workflowCalls) {
	calls := &workflowCalls{}
	return NewScopedWorkflowsUseCase(workflowsByID, updateWorkflowStub{calls}, deleteWorkflowStub{calls},
		statusWorkflowStub{calls, "activate"}, statusWorkflowStub{calls, "pause"}), calls
}

func salesScope() workflow.Scope {
	return workflow.Scope{WorkspaceID: "ws1", Departments: &wd.DepartmentFilter{DepartmentIDs: []string{"d-sales"}, WorkspaceHasDepartments: true}}
}

func TestScopedWorkflowsHideAnotherWorkspacesWorkflow(t *testing.T) {
	uc, calls := scopedFixture()
	owner := workflow.Scope{WorkspaceID: "ws1", Departments: &wd.DepartmentFilter{IsOwnerOrAdmin: true}}
	if _, err := uc.Get(owner, "w-other"); !errors.Is(err, workflow.ErrWorkflowNotFound) {
		t.Fatalf("get: %v", err)
	}
	if _, err := uc.Pause(owner, "w-other"); !errors.Is(err, workflow.ErrWorkflowNotFound) {
		t.Fatalf("pause: %v", err)
	}
	if _, err := uc.Activate(owner, "w-other"); !errors.Is(err, workflow.ErrWorkflowNotFound) {
		t.Fatalf("activate: %v", err)
	}
	if err := uc.Delete(owner, "w-other"); !errors.Is(err, workflow.ErrWorkflowNotFound) {
		t.Fatalf("delete: %v", err)
	}
	if _, err := uc.Update(owner, "w-other", &workflow.Workflow{}); !errors.Is(err, workflow.ErrWorkflowNotFound) {
		t.Fatalf("update: %v", err)
	}
	if len(calls.calls) != 0 {
		t.Fatalf("changed another workspace's workflow: %v", calls.calls)
	}
}

func TestScopedWorkflowsRefuseAnotherDepartmentsWorkflow(t *testing.T) {
	uc, calls := scopedFixture()
	if _, err := uc.Pause(salesScope(), "w-team"); !errors.Is(err, workflow.ErrWorkflowAccessDenied) || len(calls.calls) != 0 {
		t.Fatalf("err %v calls %v", err, calls.calls)
	}
}

func TestScopedWorkflowsFailClosedWithoutAScope(t *testing.T) {
	uc, calls := scopedFixture()
	if _, err := uc.Activate(workflow.Scope{WorkspaceID: "ws1"}, "w-sales"); !errors.Is(err, workflow.ErrWorkflowAccessDenied) || len(calls.calls) != 0 {
		t.Fatalf("err %v calls %v", err, calls.calls)
	}
}

func TestScopedWorkflowsRunTheChangeInsideTheScope(t *testing.T) {
	uc, calls := scopedFixture()
	if _, err := uc.Pause(salesScope(), "w-sales"); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.Activate(salesScope(), "w-sales"); err != nil {
		t.Fatal(err)
	}
	if err := uc.Delete(salesScope(), "w-sales"); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.Update(salesScope(), "w-sales", &workflow.Workflow{Name: "novo"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"pause:w-sales", "activate:w-sales", "delete:w-sales", "update:w-sales"}
	for i, c := range want {
		if calls.calls[i] != c {
			t.Fatalf("calls = %v", calls.calls)
		}
	}
}
