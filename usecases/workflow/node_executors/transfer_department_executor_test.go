package node_executors

import (
	"errors"
	"fmt"
	"testing"

	ia "vozko/domain/inbox_assignment"
	"vozko/domain/workflow"
	"vozko/domain/workspace"
	dept "vozko/domain/workspace/workspace_department"
)

type transferDeptRepoStub struct {
	dept.Repository
	department *dept.Department
}

func (s *transferDeptRepoStub) GetDepartmentByID(string) (*dept.Department, error) {
	if s.department == nil {
		return nil, errors.New("not found")
	}
	return s.department, nil
}

type rouletteHandOffStub struct {
	assignMemberHandOffMock
	got   ia.RouletteHandOff
	owner string
	err   error
	calls int
}

func (s *rouletteHandOffStub) HandOffToRoulette(in ia.RouletteHandOff) (string, error) {
	s.calls++
	s.got = in
	return s.owner, s.err
}

func transferCtx(departmentID string) *workflow.NodeContext {
	ctx := assignMemberCtx(map[string]interface{}{"department_id": departmentID}, assignMemberEdges())
	ctx.Run.WorkflowID = "wf-1"
	return ctx
}

func newTransfer(handOff *rouletteHandOffStub) workflow.NodeExecutor {
	return NewTransferDepartmentExecutor(
		&transferDeptRepoStub{department: &dept.Department{ID: "sales", WorkspaceID: "ws1", Name: "Vendas"}},
		&assignMemberWorkspaceMock{member: &workspace.Member{UserID: "user-9", Email: "nine@example.com"}},
		handOff,
	)
}

func TestTransferDepartment_DealsThroughTheSharedRoulette(t *testing.T) {
	// The same ring the first customer message uses, not a private rotation:
	// the workspace's mode, eligibility and pointer all apply.
	handOff := &rouletteHandOffStub{owner: "user-9"}

	result, err := newTransfer(handOff).Execute(transferCtx("sales"))

	if err != nil {
		t.Fatal(err)
	}
	want := ia.RouletteHandOff{WorkspaceID: "ws1", EntryID: "entry1", EntryType: "lead", DepartmentID: "sales", ByActorID: "workflow:wf-1"}
	if handOff.got != want {
		t.Fatalf("hand-off = %+v, want %+v", handOff.got, want)
	}
	if result.NextNodeID != "ok" || result.Output["success"] != true {
		t.Fatalf("result = %+v, want success", result)
	}
	for key, want := range map[string]interface{}{
		"assigned_user_id":    "user-9",
		"assigned_user_email": "nine@example.com",
		"department_id":       "sales",
		"department_name":     "Vendas",
		"queued":              false,
	} {
		if result.Output[key] != want {
			t.Errorf("output %s = %v, want %v", key, result.Output[key], want)
		}
	}
}

func TestTransferDepartment_WithoutADepartmentUsesTheConversations(t *testing.T) {
	handOff := &rouletteHandOffStub{owner: "user-9"}

	result, err := newTransfer(handOff).Execute(transferCtx(""))

	if err != nil {
		t.Fatal(err)
	}
	if handOff.got.DepartmentID != "" {
		t.Fatalf("department = %q, want the conversation's own (empty)", handOff.got.DepartmentID)
	}
	if result.Output["success"] != true || result.Output["department_name"] != "" {
		t.Fatalf("result = %+v", result.Output)
	}
}

func TestTransferDepartment_NobodyEligibleIsASuccessInTheTeamQueue(t *testing.T) {
	// The roulette released it to the team; the flow continues, and can branch
	// on "queued" to tell the contact someone will answer.
	result, err := newTransfer(&rouletteHandOffStub{owner: ""}).Execute(transferCtx("sales"))

	if err != nil {
		t.Fatal(err)
	}
	if result.NextNodeID != "ok" || result.Output["queued"] != true || result.Output["assigned_user_id"] != "" {
		t.Fatalf("result = %+v, want success in the team queue", result.Output)
	}
}

func TestTransferDepartment_ADepartmentOutsideTheWorkspaceTakesTheErrorEdge(t *testing.T) {
	handOff := &rouletteHandOffStub{err: fmt.Errorf("wrap: %w", ia.ErrDepartmentOutOfScope)}

	result, err := newTransfer(handOff).Execute(transferCtx("other"))

	if err != nil {
		t.Fatal(err)
	}
	if result.NextNodeID != "fail" || result.Output["success"] != false {
		t.Fatalf("result = %+v, want the error edge", result)
	}
	if result.Output["error"] != "departamento não pertence a este workspace" {
		t.Fatalf("error = %v", result.Output["error"])
	}
}

func TestTransferDepartment_WithoutAHandOffItIsUnavailable(t *testing.T) {
	// The node test runner builds executors without the live service.
	exec := NewTransferDepartmentExecutor(&transferDeptRepoStub{}, nil, nil)

	result, err := exec.Execute(transferCtx("sales"))

	if err != nil {
		t.Fatal(err)
	}
	if result.Output["success"] != false {
		t.Fatalf("result = %+v, want unavailable", result)
	}
}

func TestTransferDepartment_DepartmentIsOptional(t *testing.T) {
	def := NewTransferDepartmentExecutor(nil, nil, nil).(workflow.NodeDefiner).Definition()
	if len(def.ConfigSchema) != 1 || def.ConfigSchema[0].Required {
		t.Fatalf("department must be optional, got %+v", def.ConfigSchema)
	}
	if def.ConfigSchema[0].OptionsSource != "departments" {
		t.Fatalf("options source = %q, want departments", def.ConfigSchema[0].OptionsSource)
	}
}

func (m *assignMemberHandOffMock) HandOffToRoulette(ia.RouletteHandOff) (string, error) {
	return "", nil
}
