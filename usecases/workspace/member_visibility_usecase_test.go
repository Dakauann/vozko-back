package workspace_usecase

import (
	"context"
	"testing"

	"vozko/domain/workspace"
	workspace_department "vozko/domain/workspace/workspace_department"
	wsc "vozko/domain/workspace_config"
)

// fakeWsRepo embeds workspace.Repository (nil) and only implements the methods
// the visibility policy actually calls; any other call would panic, which keeps
// the fake honest about what the policy depends on.
type fakeWsRepo struct {
	workspace.Repository
	membersByUser map[string]*workspace.Member // userID -> member
	perms         map[string]bool              // memberID|resource|action
}

func (f *fakeWsRepo) GetMember(workspaceID, userID string) (*workspace.Member, error) {
	return f.membersByUser[userID], nil
}

func (f *fakeWsRepo) HasPermission(memberID string, r workspace.Resource, a workspace.Action) (bool, error) {
	return f.perms[memberID+"|"+string(r)+"|"+string(a)], nil
}

type fakeDeptRepo struct {
	workspace_department.Repository
	departments []workspace_department.Department
	deptsByUser map[string][]string // userID -> department IDs
}

func (f *fakeDeptRepo) ListDepartments(workspaceID string) ([]workspace_department.Department, error) {
	return f.departments, nil
}

func (f *fakeDeptRepo) GetMemberDepartmentIDs(workspaceID, userID string) ([]string, error) {
	return f.deptsByUser[userID], nil
}

type fakeConfigReader struct {
	skipAdmin bool
}

func (f *fakeConfigReader) GetByWorkspaceID(ctx context.Context, workspaceID string) (*wsc.WorkspaceConfig, error) {
	return &wsc.WorkspaceConfig{WorkspaceID: workspaceID, SkipAdminAssignment: f.skipAdmin}, nil
}

const ws = "ws1"

func permKey(memberID string, r workspace.Resource, a workspace.Action) string {
	return memberID + "|" + string(r) + "|" + string(a)
}

func TestMemberVisibilityScope(t *testing.T) {
	twoDepts := []workspace_department.Department{{ID: "A"}, {ID: "B"}}

	tests := []struct {
		name          string
		callerUserID  string
		platformAdmin bool
		members       map[string]*workspace.Member
		perms         map[string]bool
		departments   []workspace_department.Department
		deptsByUser   map[string][]string
		wantErr       bool
		wantRestrict  bool
		wantDeptIDs   []string
	}{
		{
			name:          "platform admin sees everyone",
			callerUserID:  "u1",
			platformAdmin: true,
			wantRestrict:  false,
		},
		{
			name:         "workspace owner sees everyone",
			callerUserID: "u1",
			members:      map[string]*workspace.Member{"u1": {ID: "m1", UserID: "u1", Role: workspace.RoleOwner}},
			departments:  twoDepts,
			wantRestrict: false,
		},
		{
			name:         "non-member is unauthorized",
			callerUserID: "ghost",
			members:      map[string]*workspace.Member{},
			wantErr:      true,
		},
		{
			name:         "member without members:read is unauthorized",
			callerUserID: "u1",
			members:      map[string]*workspace.Member{"u1": {ID: "m1", UserID: "u1", Role: workspace.RoleMember}},
			perms:        map[string]bool{},
			wantErr:      true,
		},
		{
			name:         "no departments means everyone visible (no self-only)",
			callerUserID: "u1",
			members:      map[string]*workspace.Member{"u1": {ID: "m1", UserID: "u1", Role: workspace.RoleMember}},
			perms:        map[string]bool{permKey("m1", workspace.ResourceMembers, workspace.ActionRead): true},
			departments:  nil,
			wantRestrict: false,
		},
		{
			name:         "view_others widens to all departments",
			callerUserID: "u1",
			members:      map[string]*workspace.Member{"u1": {ID: "m1", UserID: "u1", Role: workspace.RoleMember}},
			perms: map[string]bool{
				permKey("m1", workspace.ResourceMembers, workspace.ActionRead):       true,
				permKey("m1", workspace.ResourceMembers, workspace.ActionViewOthers): true,
			},
			departments:  twoDepts,
			deptsByUser:  map[string][]string{"u1": {"A"}},
			wantRestrict: false,
		},
		{
			name:         "scoped to own departments",
			callerUserID: "u1",
			members:      map[string]*workspace.Member{"u1": {ID: "m1", UserID: "u1", Role: workspace.RoleMember}},
			perms:        map[string]bool{permKey("m1", workspace.ResourceMembers, workspace.ActionRead): true},
			departments:  twoDepts,
			deptsByUser:  map[string][]string{"u1": {"A", "B"}},
			wantRestrict: true,
			wantDeptIDs:  []string{"A", "B"},
		},
		{
			name:         "member in zero departments is scoped (self + admins)",
			callerUserID: "u1",
			members:      map[string]*workspace.Member{"u1": {ID: "m1", UserID: "u1", Role: workspace.RoleMember}},
			perms:        map[string]bool{permKey("m1", workspace.ResourceMembers, workspace.ActionRead): true},
			departments:  twoDepts,
			deptsByUser:  map[string][]string{},
			wantRestrict: true,
			wantDeptIDs:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := NewMemberVisibilityUseCase(
				&fakeWsRepo{membersByUser: tt.members, perms: tt.perms},
				&fakeDeptRepo{departments: tt.departments, deptsByUser: tt.deptsByUser},
				&fakeConfigReader{},
			)
			scope, err := uc.Scope(tt.callerUserID, ws, tt.platformAdmin)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (scope=%+v)", scope)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if scope.Restrict != tt.wantRestrict {
				t.Fatalf("Restrict = %v, want %v", scope.Restrict, tt.wantRestrict)
			}
			if len(scope.DepartmentIDs) != len(tt.wantDeptIDs) {
				t.Fatalf("DepartmentIDs = %v, want %v", scope.DepartmentIDs, tt.wantDeptIDs)
			}
		})
	}
}

func TestMemberVisibilityCanView(t *testing.T) {
	twoDepts := []workspace_department.Department{{ID: "A"}, {ID: "B"}}

	// caller u1: plain member in dept A. u2: dept A. u3: dept B. o1: owner (no
	// dept). a1: admin (no dept). b1: departmentless regular member (idle).
	members := map[string]*workspace.Member{
		"u1": {ID: "m1", UserID: "u1", Role: workspace.RoleMember},
		"u2": {ID: "m2", UserID: "u2", Role: workspace.RoleMember},
		"u3": {ID: "m3", UserID: "u3", Role: workspace.RoleMember},
		"o1": {ID: "mo", UserID: "o1", Role: workspace.RoleOwner},
		"a1": {ID: "ma", UserID: "a1", Role: workspace.RoleAdmin},
		"b1": {ID: "mb", UserID: "b1", Role: workspace.RoleMember},
	}
	perms := map[string]bool{
		permKey("m1", workspace.ResourceMembers, workspace.ActionRead): true,
	}
	deptsByUser := map[string][]string{
		"u1": {"A"},
		"u2": {"A"},
		"u3": {"B"},
	}

	newUC := func(skipAdmin bool) workspace.MemberVisibilityUseCase {
		return NewMemberVisibilityUseCase(
			&fakeWsRepo{membersByUser: members, perms: perms},
			&fakeDeptRepo{departments: twoDepts, deptsByUser: deptsByUser},
			&fakeConfigReader{skipAdmin: skipAdmin},
		)
	}

	cases := []struct {
		name           string
		caller, target string
		platformAdmin  bool
		skipAdmin      bool
		want           bool
	}{
		{name: "self is always viewable", caller: "u1", target: "u1", want: true},
		{name: "same department is viewable", caller: "u1", target: "u2", want: true},
		{name: "other department is denied", caller: "u1", target: "u3", want: false},
		{name: "target not a member is denied", caller: "u1", target: "u9", want: false},
		{name: "owner can view any member", caller: "o1", target: "u3", want: true},
		{name: "owner cannot view non-member", caller: "o1", target: "u9", want: false},
		{name: "platform admin can view any member", caller: "x", target: "u3", platformAdmin: true, want: true},
		{name: "scoped member reaches admin when admin participates in roulette", caller: "u1", target: "a1", skipAdmin: false, want: true},
		{name: "scoped member reaches owner when admin participates in roulette", caller: "u1", target: "o1", skipAdmin: false, want: true},
		{name: "scoped member cannot reach admin when admin excluded from roulette", caller: "u1", target: "a1", skipAdmin: true, want: false},
		{name: "scoped member never reaches an idle (no-department) member", caller: "u1", target: "b1", skipAdmin: false, want: false},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := newUC(tt.skipAdmin).CanView(tt.caller, tt.target, ws, tt.platformAdmin)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ok != tt.want {
				t.Fatalf("CanView(%s -> %s) = %v, want %v", tt.caller, tt.target, ok, tt.want)
			}
		})
	}
}
