package copilottools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"vozko/domain/copilot"
	ia "vozko/domain/inbox_assignment"
	"vozko/domain/shared"
	"vozko/domain/workspace"
	wd "vozko/domain/workspace/workspace_department"
)

const assignableMember = "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d"

type fakePersonAssign struct {
	by       shared.Person
	assigned []string
	handed   []string
	err      error
}

func (f *fakePersonAssign) Assign(by shared.Person, workspaceID, entryID, entryType, toUserID string) error {
	f.by = by
	f.assigned = append(f.assigned, workspaceID+"|"+entryID+"|"+toUserID)
	return f.err
}

func (f *fakePersonAssign) HandOff(by shared.Person, workspaceID, entryID, entryType, departmentID string) (string, error) {
	f.by = by
	f.handed = append(f.handed, workspaceID+"|"+entryID+"|"+departmentID)
	return assignableMember, f.err
}

type fakeAssignable struct {
	userID string
	admin  bool
}

func (f *fakeAssignable) Execute(userID, workspaceID string, isPlatformAdmin bool, search string, page, pageSize int) ([]*workspace.AssignableMember, int64, error) {
	f.userID, f.admin = userID, isPlatformAdmin
	return []*workspace.AssignableMember{{
		Member:      &workspace.Member{UserID: assignableMember, Username: "Ana", Email: "ana@loja.com"},
		Departments: []workspace.DepartmentRef{{ID: knownDepartment, Name: "Vendas"}},
	}}, 1, nil
}

type fakeDepartmentList struct{}

func (fakeDepartmentList) Execute(string) ([]wd.Department, error) {
	return []wd.Department{{ID: knownDepartment, Name: "Vendas"}}, nil
}

func assignmentDeps(assign *fakePersonAssign, members *fakeAssignable) AssignmentDeps {
	return AssignmentDeps{Assign: assign, Members: members, Departments: fakeDepartmentList{}, Entries: &fakeEntries{}}
}

func TestAssignmentToolsCheckTheirRoutesPermissions(t *testing.T) {
	deps := assignmentDeps(&fakePersonAssign{}, &fakeAssignable{})
	cases := map[string]struct {
		perm     string
		mutating bool
	}{
		"list_assignable_members": {"members:read", false},
		"assign_conversation":     {"conversations:assign", true},
		"transfer_conversation":   {"conversations:assign", true},
	}
	for _, tool := range []copilot.Tool{NewListAssignableMembersTool(deps), NewAssignConversationTool(deps), NewTransferConversationTool(deps)} {
		m, want := tool.Meta(), cases[tool.Definition().Name]
		if string(m.Resource)+":"+string(m.Action) != want.perm || m.Mutating != want.mutating {
			t.Fatalf("%s meta = %+v", tool.Definition().Name, m)
		}
	}
}

func TestListAssignableMembersListsWhoTheUserMayAssign(t *testing.T) {
	members := &fakeAssignable{}
	res := NewListAssignableMembersTool(assignmentDeps(&fakePersonAssign{}, members)).Execute(context.Background(), member(), nil)
	b, _ := json.Marshal(res.Data)
	if res.Status != copilot.StatusOK || members.userID != "u-1" || !strings.Contains(string(b), `"name":"Ana"`) || strings.Contains(string(b), "ana@loja.com") {
		t.Fatalf("status %s data %s", res.Status, b)
	}
}

func TestAssignConversationAssignsAsTheUser(t *testing.T) {
	assign := &fakePersonAssign{}
	res := NewAssignConversationTool(assignmentDeps(assign, &fakeAssignable{})).Execute(context.Background(), member(), map[string]interface{}{
		"entry_id": knownEntry, "entry_type": "whatsapp", "member_id": assignableMember,
	})
	if res.Status != copilot.StatusOK || assign.by.UserID != "u-1" || assign.assigned[0] != "ws-1|"+knownEntry+"|"+assignableMember {
		t.Fatalf("status %s assigned %v", res.Status, assign.assigned)
	}
}

func TestAssignConversationDescribesTheMemberByName(t *testing.T) {
	fields := NewAssignConversationTool(assignmentDeps(&fakePersonAssign{}, &fakeAssignable{})).(copilot.Describer).Describe(context.Background(), member(), map[string]interface{}{
		"entry_id": knownEntry, "entry_type": "whatsapp", "member_id": assignableMember,
	})
	got := map[string]string{}
	for _, f := range fields {
		got[f.Key] = f.Value
	}
	if got["member"] != "Ana" || got["conversation"] != "Maria (••••9624)" {
		t.Fatalf("fields = %v", fields)
	}
}

func TestTransferConversationDealsFromTheDepartment(t *testing.T) {
	assign := &fakePersonAssign{}
	res := NewTransferConversationTool(assignmentDeps(assign, &fakeAssignable{})).Execute(context.Background(), member(), map[string]interface{}{
		"entry_id": knownEntry, "entry_type": "whatsapp", "department_id": knownDepartment,
	})
	if res.Status != copilot.StatusOK || assign.handed[0] != "ws-1|"+knownEntry+"|"+knownDepartment {
		t.Fatalf("status %s handed %v", res.Status, assign.handed)
	}
}

func TestAssignmentToolsExplainRefusals(t *testing.T) {
	for err, want := range map[error]copilot.Status{
		ia.ErrAssignEntryAccess:      copilot.StatusDenied,
		ia.ErrAssignTargetIneligible: copilot.StatusError,
		ia.ErrAssignTargetOutOfReach: copilot.StatusDenied,
	} {
		res := NewAssignConversationTool(assignmentDeps(&fakePersonAssign{err: err}, &fakeAssignable{})).Execute(context.Background(), member(), map[string]interface{}{
			"entry_id": knownEntry, "entry_type": "whatsapp", "member_id": assignableMember,
		})
		if res.Status != want {
			t.Fatalf("%v: status %s", err, res.Status)
		}
	}
	res := NewTransferConversationTool(assignmentDeps(&fakePersonAssign{err: ia.ErrDepartmentOutOfScope}, &fakeAssignable{})).Execute(context.Background(), member(), map[string]interface{}{
		"entry_id": knownEntry, "entry_type": "whatsapp", "department_id": knownDepartment,
	})
	if res.Status != copilot.StatusError {
		t.Fatalf("out of scope department: %s", res.Status)
	}
}

func TestAssignmentToolsRefuseInventedIds(t *testing.T) {
	assign := &fakePersonAssign{}
	NewAssignConversationTool(assignmentDeps(assign, &fakeAssignable{})).Execute(context.Background(), member(), map[string]interface{}{
		"entry_id": knownEntry, "entry_type": "whatsapp", "member_id": "ana",
	})
	NewTransferConversationTool(assignmentDeps(assign, &fakeAssignable{})).Execute(context.Background(), member(), map[string]interface{}{
		"entry_id": knownEntry, "entry_type": "whatsapp", "department_id": "vendas",
	})
	if len(assign.assigned)+len(assign.handed) != 0 {
		t.Fatal("acted with invented ids")
	}
}
