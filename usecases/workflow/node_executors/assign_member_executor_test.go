package node_executors

import (
	"errors"
	"testing"

	ia "vozko/domain/inbox_assignment"
	"vozko/domain/workflow"
	"vozko/domain/workspace"
)

type assignMemberWorkspaceMock struct {
	member    *workspace.Member
	memberErr error
}

func (m *assignMemberWorkspaceMock) GetMember(_, _ string) (*workspace.Member, error) {
	return m.member, m.memberErr
}

func (m *assignMemberWorkspaceMock) WithTx(interface{}) workspace.Repository    { return m }
func (m *assignMemberWorkspaceMock) CreateWorkspace(*workspace.Workspace) error { return nil }
func (m *assignMemberWorkspaceMock) ListAllWorkspaceIDs() ([]string, error)     { return nil, nil }
func (m *assignMemberWorkspaceMock) GetWorkspaceByID(string) (*workspace.Workspace, error) {
	return nil, nil
}
func (m *assignMemberWorkspaceMock) GetDefaultWorkspace(string) (*workspace.Workspace, error) {
	return nil, nil
}
func (m *assignMemberWorkspaceMock) ListWorkspacesByUser(string, string, string) ([]*workspace.Workspace, error) {
	return nil, nil
}
func (m *assignMemberWorkspaceMock) ListAllWorkspaces(string, string, int, int) ([]*workspace.Workspace, int64, error) {
	return nil, 0, nil
}
func (m *assignMemberWorkspaceMock) CountMembersByWorkspaceIDs([]string) (map[string]int, error) {
	return nil, nil
}
func (m *assignMemberWorkspaceMock) ListMembersPaginated(string, int, int) ([]*workspace.Member, int64, error) {
	return nil, 0, nil
}
func (m *assignMemberWorkspaceMock) UpdateWorkspace(*workspace.Workspace) error      { return nil }
func (m *assignMemberWorkspaceMock) TransferOwnership(string, string) error          { return nil }
func (m *assignMemberWorkspaceMock) DetachUserAuthoredRefs(string) error             { return nil }
func (m *assignMemberWorkspaceMock) AddMember(*workspace.Member) error               { return nil }
func (m *assignMemberWorkspaceMock) GetMemberByID(string) (*workspace.Member, error) { return nil, nil }
func (m *assignMemberWorkspaceMock) ListMembers(string) ([]*workspace.Member, error) { return nil, nil }
func (m *assignMemberWorkspaceMock) ListAssignableMembers(string, string, bool, []string, bool, string, int, int) ([]*workspace.Member, int64, error) {
	return nil, 0, nil
}
func (m *assignMemberWorkspaceMock) ListMemberDepartments(string, []string, []string) (map[string][]workspace.DepartmentRef, error) {
	return nil, nil
}
func (m *assignMemberWorkspaceMock) UpdateMemberRole(string, workspace.Role) error { return nil }
func (m *assignMemberWorkspaceMock) UpdateMemberRingChannels(string, []workspace.RingChannel) error {
	return nil
}
func (m *assignMemberWorkspaceMock) UpdateMemberRoleID(string, string) error   { return nil }
func (m *assignMemberWorkspaceMock) RemoveMember(string) error                 { return nil }
func (m *assignMemberWorkspaceMock) AddPermission(*workspace.Permission) error { return nil }
func (m *assignMemberWorkspaceMock) RemovePermission(string, workspace.Resource, workspace.Action) error {
	return nil
}
func (m *assignMemberWorkspaceMock) GetPermissions(string) ([]*workspace.Permission, error) {
	return nil, nil
}
func (m *assignMemberWorkspaceMock) HasPermission(string, workspace.Resource, workspace.Action) (bool, error) {
	return false, nil
}
func (m *assignMemberWorkspaceMock) SetPermissions(string, []*workspace.Permission) error { return nil }
func (m *assignMemberWorkspaceMock) CreateInvite(*workspace.Invite) error                 { return nil }
func (m *assignMemberWorkspaceMock) GetInviteByID(string) (*workspace.Invite, error)      { return nil, nil }
func (m *assignMemberWorkspaceMock) GetInviteByToken(string) (*workspace.Invite, error) {
	return nil, nil
}
func (m *assignMemberWorkspaceMock) ListInvitesByWorkspace(string) ([]*workspace.Invite, error) {
	return nil, nil
}
func (m *assignMemberWorkspaceMock) ListInvitesByEmail(string) ([]*workspace.Invite, error) {
	return nil, nil
}
func (m *assignMemberWorkspaceMock) UpdateInviteStatus(string, workspace.InviteStatus) error {
	return nil
}
func (m *assignMemberWorkspaceMock) PendingInviteExists(string, string) (bool, error) {
	return false, nil
}
func (m *assignMemberWorkspaceMock) GetWorkspaceIDForResource(string, string) (string, error) {
	return "", nil
}
func (m *assignMemberWorkspaceMock) AssignResource(*workspace.ResourceAssignment) error { return nil }
func (m *assignMemberWorkspaceMock) UnassignResource(string, workspace.Resource, string, string) error {
	return nil
}
func (m *assignMemberWorkspaceMock) ListAssignmentsByResource(string, workspace.Resource, string) ([]*workspace.ResourceAssignment, error) {
	return nil, nil
}
func (m *assignMemberWorkspaceMock) ListAssignmentsByMember(string, workspace.Resource) ([]*workspace.ResourceAssignment, error) {
	return nil, nil
}
func (m *assignMemberWorkspaceMock) IsResourceAssignedToMember(string, workspace.Resource, string, string) (bool, error) {
	return false, nil
}
func (m *assignMemberWorkspaceMock) HasAnyAssignments(string, workspace.Resource, string) (bool, error) {
	return false, nil
}

type assignMemberAssignmentMock struct {
	assignErr      error
	lastAssignment *ia.InboxAssignment
}

func (m *assignMemberAssignmentMock) Assign(a *ia.InboxAssignment) error {
	m.lastAssignment = a
	return m.assignErr
}
func (m *assignMemberAssignmentMock) FindByEntry(string, string, string) (*ia.InboxAssignment, error) {
	return nil, nil
}
func (m *assignMemberAssignmentMock) FindByEntries(string, []string) ([]*ia.InboxAssignment, error) {
	return nil, nil
}
func (m *assignMemberAssignmentMock) FindByEntryAndUser(string, string, string, string) (*ia.InboxAssignment, error) {
	return nil, nil
}
func (m *assignMemberAssignmentMock) IsAssignedToUser(string, string, string, string) (bool, error) {
	return false, nil
}
func (m *assignMemberAssignmentMock) Unassign(string, string, string) error { return nil }
func (m *assignMemberAssignmentMock) ListByUser(string, string, string) ([]string, error) {
	return nil, nil
}
func (m *assignMemberAssignmentMock) GetRoundRobinState(string, string, string) (*ia.RoundRobinState, error) {
	return nil, nil
}
func (m *assignMemberAssignmentMock) SaveRoundRobinState(*ia.RoundRobinState) error { return nil }

func assignMemberCtx(config map[string]interface{}, edges []workflow.Edge) *workflow.NodeContext {
	state := workflow.NewRunState()
	return &workflow.NodeContext{
		Node: &workflow.Node{
			ID:     "n1",
			Config: config,
		},
		Graph: &workflow.Graph{Edges: edges},
		Run: &workflow.WorkflowRun{
			ID:          "run1",
			WorkspaceID: "ws1",
			EntryID:     "entry1",
			EntryType:   "lead",
		},
		State: &state,
	}
}

func assignMemberEdges() []workflow.Edge {
	return []workflow.Edge{
		{Source: "n1", Target: "ok", Label: "sucesso"},
		{Source: "n1", Target: "fail", Label: "erro"},
	}
}

func TestAssignMember_Success(t *testing.T) {
	wsMock := &assignMemberWorkspaceMock{
		member: &workspace.Member{UserID: "user-123", Email: "john@example.com"},
	}
	iaMock := &assignMemberAssignmentMock{}
	exec := NewAssignMemberExecutor(wsMock, iaMock)

	ctx := assignMemberCtx(map[string]interface{}{"member_id": "user-123"}, assignMemberEdges())
	result, err := exec.Execute(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NextNodeID != "ok" {
		t.Errorf("NextNodeID = %q, want ok", result.NextNodeID)
	}
	if result.Output["success"] != true {
		t.Error("expected success=true")
	}
	if result.Output["assigned_user_id"] != "user-123" {
		t.Errorf("assigned_user_id = %v, want user-123", result.Output["assigned_user_id"])
	}
	if result.Output["assigned_user_email"] != "john@example.com" {
		t.Errorf("assigned_user_email = %v, want john@example.com", result.Output["assigned_user_email"])
	}
	if iaMock.lastAssignment == nil {
		t.Fatal("expected assignment to be created")
	}
	if iaMock.lastAssignment.AssignedUserID != "user-123" {
		t.Errorf("assignment.AssignedUserID = %q, want user-123", iaMock.lastAssignment.AssignedUserID)
	}
}

func TestAssignMember_MemberNotFound(t *testing.T) {
	wsMock := &assignMemberWorkspaceMock{member: nil, memberErr: nil}
	iaMock := &assignMemberAssignmentMock{}
	exec := NewAssignMemberExecutor(wsMock, iaMock)

	ctx := assignMemberCtx(map[string]interface{}{"member_id": "nonexistent"}, assignMemberEdges())
	result, err := exec.Execute(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NextNodeID != "fail" {
		t.Errorf("NextNodeID = %q, want fail", result.NextNodeID)
	}
	if result.Output["success"] != false {
		t.Error("expected success=false")
	}
}

func TestAssignMember_MemberLookupError(t *testing.T) {
	wsMock := &assignMemberWorkspaceMock{member: nil, memberErr: errors.New("db error")}
	iaMock := &assignMemberAssignmentMock{}
	exec := NewAssignMemberExecutor(wsMock, iaMock)

	ctx := assignMemberCtx(map[string]interface{}{"member_id": "user-123"}, assignMemberEdges())
	result, err := exec.Execute(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NextNodeID != "fail" {
		t.Errorf("NextNodeID = %q, want fail", result.NextNodeID)
	}
	if result.Output["success"] != false {
		t.Error("expected success=false")
	}
}

func TestAssignMember_AssignmentError(t *testing.T) {
	wsMock := &assignMemberWorkspaceMock{
		member: &workspace.Member{UserID: "user-123", Email: "john@example.com"},
	}
	iaMock := &assignMemberAssignmentMock{assignErr: errors.New("assign failed")}
	exec := NewAssignMemberExecutor(wsMock, iaMock)

	ctx := assignMemberCtx(map[string]interface{}{"member_id": "user-123"}, assignMemberEdges())
	result, err := exec.Execute(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NextNodeID != "fail" {
		t.Errorf("NextNodeID = %q, want fail", result.NextNodeID)
	}
	if result.Output["success"] != false {
		t.Error("expected success=false")
	}
}

func TestAssignMember_MissingConfig(t *testing.T) {
	wsMock := &assignMemberWorkspaceMock{}
	iaMock := &assignMemberAssignmentMock{}
	exec := NewAssignMemberExecutor(wsMock, iaMock)

	ctx := assignMemberCtx(map[string]interface{}{}, assignMemberEdges())
	_, err := exec.Execute(ctx)
	if !errors.Is(err, workflow.ErrNodeConfigMissing) {
		t.Errorf("expected ErrNodeConfigMissing, got %v", err)
	}
}

func TestAssignMember_EmptyMemberID(t *testing.T) {
	wsMock := &assignMemberWorkspaceMock{}
	iaMock := &assignMemberAssignmentMock{}
	exec := NewAssignMemberExecutor(wsMock, iaMock)

	ctx := assignMemberCtx(map[string]interface{}{"member_id": "   "}, assignMemberEdges())
	_, err := exec.Execute(ctx)
	if !errors.Is(err, workflow.ErrNodeConfigMissing) {
		t.Errorf("expected ErrNodeConfigMissing, got %v", err)
	}
}

func TestAssignMember_InterpolatedVariable(t *testing.T) {
	wsMock := &assignMemberWorkspaceMock{
		member: &workspace.Member{UserID: "user-456", Email: "jane@example.com"},
	}
	iaMock := &assignMemberAssignmentMock{}
	exec := NewAssignMemberExecutor(wsMock, iaMock)

	ctx := assignMemberCtx(map[string]interface{}{"member_id": "{{vars.target_member}}"}, assignMemberEdges())
	ctx.State.Set("vars", map[string]interface{}{"target_member": "user-456"})
	result, err := exec.Execute(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NextNodeID != "ok" {
		t.Errorf("NextNodeID = %q, want ok", result.NextNodeID)
	}
	if result.Output["assigned_user_id"] != "user-456" {
		t.Errorf("assigned_user_id = %v, want user-456", result.Output["assigned_user_id"])
	}
}

func TestAssignMember_Definition(t *testing.T) {
	exec := NewAssignMemberExecutor(nil, nil)
	definer, ok := exec.(workflow.NodeDefiner)
	if !ok {
		t.Fatal("executor should implement NodeDefiner")
	}
	def := definer.Definition()
	if def.Type != workflow.NodeTypeActionAssignMember {
		t.Errorf("Type = %q, want %q", def.Type, workflow.NodeTypeActionAssignMember)
	}
	if def.Category != workflow.NodeCategoryAction {
		t.Errorf("Category = %q, want %q", def.Category, workflow.NodeCategoryAction)
	}
	if len(def.Outputs) != 2 {
		t.Errorf("Outputs count = %d, want 2", len(def.Outputs))
	}
	if len(def.ConfigSchema) != 1 {
		t.Errorf("ConfigSchema count = %d, want 1", len(def.ConfigSchema))
	}
	if def.ConfigSchema[0].OptionsSource != "members" {
		t.Errorf("ConfigSchema[0].OptionsSource = %q, want members", def.ConfigSchema[0].OptionsSource)
	}
}

func TestAssignMember_NoErrorEdge(t *testing.T) {
	wsMock := &assignMemberWorkspaceMock{member: nil, memberErr: nil}
	iaMock := &assignMemberAssignmentMock{}
	exec := NewAssignMemberExecutor(wsMock, iaMock)

	edges := []workflow.Edge{
		{Source: "n1", Target: "ok", Label: "sucesso"},
	}
	ctx := assignMemberCtx(map[string]interface{}{"member_id": "nonexistent"}, edges)
	result, err := exec.Execute(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.NextNodeID != "ok" {
		t.Errorf("NextNodeID = %q, want ok (fallback)", result.NextNodeID)
	}
	if result.Output["success"] != false {
		t.Error("expected success=false")
	}
}
