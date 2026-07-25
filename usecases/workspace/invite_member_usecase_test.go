package workspace_usecase

import (
	"errors"
	"testing"

	"vozko/domain/user"
	"vozko/domain/workspace"
	wd "vozko/domain/workspace/workspace_department"
)

type stubInviteRepo struct {
	workspace.Repository

	members        map[string]*workspace.Member
	permissions    map[string]bool
	pendingInvites map[string]bool
	workspaces     map[string]*workspace.Workspace

	createdInvite *workspace.Invite
}

func newStubInviteRepo() *stubInviteRepo {
	return &stubInviteRepo{
		members:        map[string]*workspace.Member{},
		permissions:    map[string]bool{},
		pendingInvites: map[string]bool{},
		workspaces:     map[string]*workspace.Workspace{},
	}
}

func (s *stubInviteRepo) GetMember(workspaceID, userID string) (*workspace.Member, error) {
	if m, ok := s.members[workspaceID+":"+userID]; ok {
		return m, nil
	}
	return nil, nil
}

func (s *stubInviteRepo) HasPermission(memberID string, resource workspace.Resource, action workspace.Action) (bool, error) {
	return s.permissions[memberID+":"+string(resource)+":"+string(action)], nil
}

func (s *stubInviteRepo) GetWorkspaceByID(id string) (*workspace.Workspace, error) {
	if ws, ok := s.workspaces[id]; ok {
		return ws, nil
	}
	return nil, errors.New("workspace not found")
}

func (s *stubInviteRepo) PendingInviteExists(workspaceID, email string) (bool, error) {
	return s.pendingInvites[workspaceID+":"+email], nil
}

func (s *stubInviteRepo) CreateInvite(invite *workspace.Invite) error {
	s.createdInvite = invite
	return nil
}

type stubUserRepo struct {
	user.UserRepository

	byID    map[string]*user.User
	byEmail map[string]*user.User
}

func (s *stubUserRepo) FindByID(id string) (*user.User, error) {
	if u, ok := s.byID[id]; ok {
		return u, nil
	}
	return nil, nil
}

func (s *stubUserRepo) FindByEmail(email string) (*user.User, error) {
	if u, ok := s.byEmail[email]; ok {
		return u, nil
	}
	return nil, nil
}

type stubCustomRoleRepo struct {
	roles map[string]*workspace.CustomRole
}

func (s *stubCustomRoleRepo) CreateRole(role *workspace.CustomRole) error { return nil }
func (s *stubCustomRoleRepo) GetRoleByID(id string) (*workspace.CustomRole, error) {
	if r, ok := s.roles[id]; ok {
		return r, nil
	}
	return nil, errors.New("role not found")
}
func (s *stubCustomRoleRepo) ListRolesByWorkspace(workspaceID string) ([]*workspace.CustomRole, error) {
	return nil, nil
}
func (s *stubCustomRoleRepo) UpdateRole(role *workspace.CustomRole) error { return nil }
func (s *stubCustomRoleRepo) DeleteRole(id string) error                  { return nil }
func (s *stubCustomRoleRepo) ListMembersByRoleID(roleID string) ([]*workspace.Member, error) {
	return nil, nil
}

type stubDeptRepo struct {
	wd.Repository
}

type stubEmailService struct{}

func (s *stubEmailService) SendEmail(to, subject, body string) error { return nil }
func (s *stubEmailService) SendTemplate(to, subject, templateName string, data map[string]interface{}) error {
	return nil
}

const (
	testWS      = "ws-1"
	testCaller  = "user-caller"
	testInvitee = "invitee@example.com"
)

func newTestUseCase() (*stubInviteRepo, *stubUserRepo, workspace.InviteMemberUseCase) {
	repo := newStubInviteRepo()
	repo.workspaces[testWS] = &workspace.Workspace{ID: testWS, Name: "WS"}

	users := &stubUserRepo{
		byID: map[string]*user.User{
			testCaller: {ID: testCaller, Email: "caller@example.com"},
		},
		byEmail: map[string]*user.User{},
	}

	roles := &stubCustomRoleRepo{
		roles: map[string]*workspace.CustomRole{
			"custom-role-1": {ID: "custom-role-1", WorkspaceID: testWS, Name: "Agent"},
		},
	}

	uc := NewInviteMemberUseCase(repo, roles, users, &stubDeptRepo{}, &stubEmailService{})
	return repo, users, uc
}

func addMember(repo *stubInviteRepo, workspaceID, userID, memberID string, role workspace.Role) {
	repo.members[workspaceID+":"+userID] = &workspace.Member{
		ID:          memberID,
		WorkspaceID: workspaceID,
		UserID:      userID,
		Role:        role,
	}
}

func TestInviteMember_NonAdmin_CannotInviteAsAdmin(t *testing.T) {
	repo, _, uc := newTestUseCase()
	addMember(repo, testWS, testCaller, "member-1", workspace.RoleMember)
	repo.permissions["member-1:"+string(workspace.ResourceMembers)+":"+string(workspace.ActionCreate)] = true

	_, err := uc.Execute(testCaller, testWS, "user", workspace.InviteMemberInput{
		Email: testInvitee,
		Role:  workspace.RoleAdmin,
	})

	if !errors.Is(err, workspace.ErrInsufficientPermissions) {
		t.Fatalf("expected ErrInsufficientPermissions, got %v", err)
	}
	if repo.createdInvite != nil {
		t.Fatalf("invite must NOT be created on escalation attempt")
	}
}

func TestInviteMember_NonAdmin_CanInviteWithCustomRole(t *testing.T) {
	repo, _, uc := newTestUseCase()
	addMember(repo, testWS, testCaller, "member-1", workspace.RoleMember)
	repo.permissions["member-1:"+string(workspace.ResourceMembers)+":"+string(workspace.ActionCreate)] = true

	inv, err := uc.Execute(testCaller, testWS, "user", workspace.InviteMemberInput{
		Email:  testInvitee,
		RoleID: "custom-role-1",
	})

	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if inv == nil || repo.createdInvite == nil {
		t.Fatalf("expected invite to be created")
	}
	if inv.Role != "" {
		t.Fatalf("expected system role to be cleared when custom role is used, got %q", inv.Role)
	}
}

func TestInviteMember_NonAdmin_CanInviteAsMember(t *testing.T) {
	repo, _, uc := newTestUseCase()
	addMember(repo, testWS, testCaller, "member-1", workspace.RoleMember)
	repo.permissions["member-1:"+string(workspace.ResourceMembers)+":"+string(workspace.ActionCreate)] = true

	_, err := uc.Execute(testCaller, testWS, "user", workspace.InviteMemberInput{
		Email: testInvitee,
		Role:  workspace.RoleMember,
	})

	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if repo.createdInvite == nil {
		t.Fatalf("expected invite to be created")
	}
}

func TestInviteMember_NoPermission_CannotInvite(t *testing.T) {
	repo, _, uc := newTestUseCase()
	addMember(repo, testWS, testCaller, "member-1", workspace.RoleMember)

	_, err := uc.Execute(testCaller, testWS, "user", workspace.InviteMemberInput{
		Email:  testInvitee,
		RoleID: "custom-role-1",
	})

	if !errors.Is(err, workspace.ErrInsufficientPermissions) {
		t.Fatalf("expected ErrInsufficientPermissions, got %v", err)
	}
}

func TestInviteMember_NotMember_Unauthorized(t *testing.T) {
	_, _, uc := newTestUseCase()

	_, err := uc.Execute(testCaller, testWS, "user", workspace.InviteMemberInput{
		Email: testInvitee,
		Role:  workspace.RoleMember,
	})

	if !errors.Is(err, workspace.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

func TestInviteMember_WorkspaceAdmin_CanInviteAsAdmin(t *testing.T) {
	repo, _, uc := newTestUseCase()
	addMember(repo, testWS, testCaller, "member-1", workspace.RoleAdmin)

	_, err := uc.Execute(testCaller, testWS, "user", workspace.InviteMemberInput{
		Email: testInvitee,
		Role:  workspace.RoleAdmin,
	})

	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if repo.createdInvite == nil || repo.createdInvite.Role != workspace.RoleAdmin {
		t.Fatalf("expected admin invite to be created")
	}
}

func TestInviteMember_WorkspaceOwner_CanInviteAsAdmin(t *testing.T) {
	repo, _, uc := newTestUseCase()
	addMember(repo, testWS, testCaller, "member-1", workspace.RoleOwner)

	_, err := uc.Execute(testCaller, testWS, "user", workspace.InviteMemberInput{
		Email: testInvitee,
		Role:  workspace.RoleAdmin,
	})

	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if repo.createdInvite == nil || repo.createdInvite.Role != workspace.RoleAdmin {
		t.Fatalf("expected admin invite to be created")
	}
}

func TestInviteMember_PlatformAdmin_CanInviteAsAdmin(t *testing.T) {
	repo, _, uc := newTestUseCase()

	_, err := uc.Execute(testCaller, testWS, "admin", workspace.InviteMemberInput{
		Email: testInvitee,
		Role:  workspace.RoleAdmin,
	})

	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if repo.createdInvite == nil {
		t.Fatalf("expected invite to be created")
	}
}

func TestInviteMember_CannotInviteAsOwner(t *testing.T) {
	repo, _, uc := newTestUseCase()
	addMember(repo, testWS, testCaller, "member-1", workspace.RoleOwner)

	_, err := uc.Execute(testCaller, testWS, "user", workspace.InviteMemberInput{
		Email: testInvitee,
		Role:  workspace.RoleOwner,
	})

	if !errors.Is(err, workspace.ErrInvalidRole) {
		t.Fatalf("expected ErrInvalidRole, got %v", err)
	}
	if repo.createdInvite != nil {
		t.Fatalf("invite must NOT be created when role is owner")
	}
}

func TestInviteMember_PlatformAdmin_CannotInviteAsOwner(t *testing.T) {
	_, _, uc := newTestUseCase()

	_, err := uc.Execute(testCaller, testWS, "admin", workspace.InviteMemberInput{
		Email: testInvitee,
		Role:  workspace.RoleOwner,
	})

	if !errors.Is(err, workspace.ErrInvalidRole) {
		t.Fatalf("expected ErrInvalidRole, got %v", err)
	}
}

func TestInviteMember_NonAdmin_AdminRoleBlocked_RegardlessOfPermission(t *testing.T) {
	repo, _, uc := newTestUseCase()
	addMember(repo, testWS, testCaller, "member-1", workspace.RoleMember)

	repo.permissions["member-1:"+string(workspace.ResourceMembers)+":"+string(workspace.ActionCreate)] = true
	repo.permissions["member-1:"+string(workspace.ResourceMembers)+":"+string(workspace.ActionUpdate)] = true
	repo.permissions["member-1:"+string(workspace.ResourceMembers)+":"+string(workspace.ActionDelete)] = true

	_, err := uc.Execute(testCaller, testWS, "user", workspace.InviteMemberInput{
		Email: testInvitee,
		Role:  workspace.RoleAdmin,
	})

	if !errors.Is(err, workspace.ErrInsufficientPermissions) {
		t.Fatalf("expected ErrInsufficientPermissions, got %v", err)
	}
}

func TestInviteMember_CannotInviteSelf(t *testing.T) {
	repo, users, uc := newTestUseCase()
	addMember(repo, testWS, testCaller, "member-1", workspace.RoleAdmin)
	users.byID[testCaller].Email = "caller@example.com"

	_, err := uc.Execute(testCaller, testWS, "user", workspace.InviteMemberInput{
		Email: "caller@example.com",
		Role:  workspace.RoleMember,
	})

	if !errors.Is(err, workspace.ErrCannotInviteSelf) {
		t.Fatalf("expected ErrCannotInviteSelf, got %v", err)
	}
}
