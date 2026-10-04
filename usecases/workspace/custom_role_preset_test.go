package workspace_usecase

import (
	"errors"
	"testing"

	"vozko/domain/workspace"
)

type memberPermissionRepo struct {
	ownerRepo
	set map[string][]*workspace.Permission
	err error
}

func (r *memberPermissionRepo) SetPermissions(memberID string, perms []*workspace.Permission) error {
	if r.err != nil {
		return r.err
	}
	if r.set == nil {
		r.set = map[string][]*workspace.Permission{}
	}
	r.set[memberID] = perms
	return nil
}

type linkedRoleStore struct {
	roleStore
	members []*workspace.Member
	saved   []*workspace.CustomRole
}

func (s *linkedRoleStore) CreateRole(r *workspace.CustomRole) error {
	s.saved = append(s.saved, r)
	return nil
}

func (s *linkedRoleStore) UpdateRole(r *workspace.CustomRole) error {
	s.saved = append(s.saved, r)
	for i, existing := range s.roles {
		if existing.ID == r.ID {
			copied := *r
			s.roles[i] = &copied
		}
	}
	return nil
}

func (s *linkedRoleStore) ListMembersByRoleID(string) ([]*workspace.Member, error) {
	return s.members, nil
}

func (s *linkedRoleStore) ListLinkedRoles() ([]*workspace.CustomRole, error) {
	var out []*workspace.CustomRole
	for _, r := range s.roles {
		if r.Linked {
			copied := *r
			out = append(out, &copied)
		}
	}
	return out, nil
}

func presetPermissions(t *testing.T, key workspace.RolePresetKey) []workspace.PermissionEntry {
	t.Helper()
	preset, _ := workspace.RolePresetByKey(key)
	perms, err := preset.Permissions()
	if err != nil {
		t.Fatal(err)
	}
	return perms
}

func TestALinkedRoleIsCreatedWithThePresetPermissionsWhateverTheClientSends(t *testing.T) {
	store := &linkedRoleStore{}
	role, err := NewCreateCustomRoleUseCase(ownerRepo{}, store).Execute("u1", "ws", "", workspace.CreateCustomRoleInput{
		Name: "Atendimento", PresetKey: workspace.PresetOperator, Linked: true,
		Permissions: []workspace.PermissionEntry{{Resource: workspace.ResourceRoles, Action: workspace.ActionDelete}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !role.Linked || role.PresetKey != workspace.PresetOperator || !workspace.SamePermissions(role.Permissions, presetPermissions(t, workspace.PresetOperator)) {
		t.Fatalf("role = %+v", role)
	}
}

func TestACopiedRoleKeepsTheSentPermissionsAndRemembersItsPreset(t *testing.T) {
	store := &linkedRoleStore{}
	sent := []workspace.PermissionEntry{{Resource: workspace.ResourceConversations, Action: workspace.ActionRead}}
	role, err := NewCreateCustomRoleUseCase(ownerRepo{}, store).Execute("u1", "ws", "", workspace.CreateCustomRoleInput{
		Name: "Atendimento leve", PresetKey: workspace.PresetOperator, Permissions: sent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if role.Linked || role.PresetKey != workspace.PresetOperator || !workspace.SamePermissions(role.Permissions, sent) {
		t.Fatalf("role = %+v", role)
	}
}

func TestAnUnknownPresetIsRefusedOnCreate(t *testing.T) {
	store := &linkedRoleStore{}
	_, err := NewCreateCustomRoleUseCase(ownerRepo{}, store).Execute("u1", "ws", "", workspace.CreateCustomRoleInput{
		Name: "X", PresetKey: "ghost", Linked: true,
	})
	if !errors.Is(err, workspace.ErrUnknownRolePreset) || len(store.saved) != 0 {
		t.Fatalf("err = %v saved = %d", err, len(store.saved))
	}
}

func linkedOperator() *linkedRoleStore {
	return &linkedRoleStore{
		roleStore: roleStore{roles: []*workspace.CustomRole{{
			ID: "r1", WorkspaceID: "ws", Name: "Atendimento", PresetKey: workspace.PresetOperator, Linked: true,
		}}},
		members: []*workspace.Member{{ID: "m1"}, {ID: "m2"}},
	}
}

func TestEditingALinkedRolesPermissionsIsRefused(t *testing.T) {
	store := linkedOperator()
	_, err := NewUpdateCustomRoleUseCase(ownerRepo{}, store).Execute("u1", "ws", "", "r1", workspace.UpdateCustomRoleInput{
		Permissions: []workspace.PermissionEntry{{Resource: workspace.ResourceRoles, Action: workspace.ActionDelete}},
	})
	if !errors.Is(err, workspace.ErrLinkedRoleLocked) || len(store.saved) != 0 {
		t.Fatalf("err = %v saved = %d", err, len(store.saved))
	}
}

func TestUnlinkingLetsTheRoleBeEditedInTheSameRequest(t *testing.T) {
	store := linkedOperator()
	repo := &memberPermissionRepo{}
	off := false
	sent := []workspace.PermissionEntry{{Resource: workspace.ResourceConversations, Action: workspace.ActionRead}}
	role, err := NewUpdateCustomRoleUseCase(repo, store).Execute("u1", "ws", "", "r1", workspace.UpdateCustomRoleInput{Linked: &off, Permissions: sent})
	if err != nil {
		t.Fatal(err)
	}
	if role.Linked || role.PresetKey != workspace.PresetOperator || !workspace.SamePermissions(role.Permissions, sent) {
		t.Fatalf("role = %+v", role)
	}
	if len(repo.set) != 2 {
		t.Fatalf("members updated = %d, want 2", len(repo.set))
	}
}

func TestRelinkingResetsThePermissionsToThePreset(t *testing.T) {
	store := linkedOperator()
	store.roles[0].Linked = false
	store.roles[0].Permissions = []workspace.PermissionEntry{{Resource: workspace.ResourceConversations, Action: workspace.ActionRead}}
	on := true
	role, err := NewUpdateCustomRoleUseCase(&memberPermissionRepo{}, store).Execute("u1", "ws", "", "r1", workspace.UpdateCustomRoleInput{Linked: &on})
	if err != nil {
		t.Fatal(err)
	}
	if !role.Linked || !workspace.SamePermissions(role.Permissions, presetPermissions(t, workspace.PresetOperator)) {
		t.Fatalf("role = %+v", role)
	}
}

func TestAMemberUpdateFailureIsReportedNotSwallowed(t *testing.T) {
	store := linkedOperator()
	store.roles[0].Linked = false
	repo := &memberPermissionRepo{err: errors.New("db down")}
	_, err := NewUpdateCustomRoleUseCase(repo, store).Execute("u1", "ws", "", "r1", workspace.UpdateCustomRoleInput{
		Permissions: []workspace.PermissionEntry{{Resource: workspace.ResourceConversations, Action: workspace.ActionRead}},
	})
	if err == nil {
		t.Fatal("members left on the old permissions must surface as an error")
	}
}

func TestSyncBringsLinkedRolesAndTheirMembersUpToTheirPreset(t *testing.T) {
	store := linkedOperator()
	store.roles[0].Permissions = []workspace.PermissionEntry{{Resource: workspace.ResourceConversations, Action: workspace.ActionRead}}
	repo := &memberPermissionRepo{}

	report, err := NewSyncLinkedRolesUseCase(repo, store).Execute()
	if err != nil {
		t.Fatal(err)
	}
	if report.Updated != 1 || len(repo.set) != 2 {
		t.Fatalf("report = %+v, members updated = %d", report, len(repo.set))
	}
	if again, _ := NewSyncLinkedRolesUseCase(repo, store).Execute(); again.Updated != 0 {
		t.Fatalf("a second sync changed %d roles, want 0", again.Updated)
	}
}

func TestSyncDetachesRolesWhosePresetWasRetiredWithoutGrantingAnything(t *testing.T) {
	store := linkedOperator()
	kept := []workspace.PermissionEntry{{Resource: workspace.ResourceConversations, Action: workspace.ActionRead}}
	store.roles[0].PresetKey = "retired"
	store.roles[0].Permissions = kept
	repo := &memberPermissionRepo{}

	report, err := NewSyncLinkedRolesUseCase(repo, store).Execute()
	if err != nil {
		t.Fatal(err)
	}
	if report.Detached != 1 || store.roles[0].Linked || !workspace.SamePermissions(store.roles[0].Permissions, kept) || len(repo.set) != 0 {
		t.Fatalf("report = %+v role = %+v members = %d", report, store.roles[0], len(repo.set))
	}
}
