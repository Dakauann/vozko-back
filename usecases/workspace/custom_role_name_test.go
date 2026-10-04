package workspace_usecase

import (
	"errors"
	"testing"

	"vozko/domain/workspace"
)

type ownerRepo struct{ workspace.Repository }

func (ownerRepo) GetMember(workspaceID, userID string) (*workspace.Member, error) {
	return &workspace.Member{WorkspaceID: workspaceID, UserID: userID, Role: workspace.RoleOwner}, nil
}

type roleStore struct {
	workspace.CustomRoleRepository
	roles   []*workspace.CustomRole
	created int
	updated int
}

func (s *roleStore) ListRolesByWorkspace(string) ([]*workspace.CustomRole, error) {
	return s.roles, nil
}
func (s *roleStore) CreateRole(*workspace.CustomRole) error { s.created++; return nil }
func (s *roleStore) UpdateRole(*workspace.CustomRole) error { s.updated++; return nil }
func (s *roleStore) GetRoleByID(id string) (*workspace.CustomRole, error) {
	for _, r := range s.roles {
		if r.ID == id {
			copied := *r
			return &copied, nil
		}
	}
	return nil, workspace.ErrRoleNotFound
}

func existingRoles() *roleStore {
	return &roleStore{roles: []*workspace.CustomRole{
		{ID: "r1", WorkspaceID: "ws", Name: "Operador de atendimento"},
		{ID: "r2", WorkspaceID: "ws", Name: "Gerente"},
	}}
}

func TestCreatingARoleWithATakenNameIsRefused(t *testing.T) {
	store := existingRoles()
	_, err := NewCreateCustomRoleUseCase(ownerRepo{}, store).Execute("u1", "ws", "", workspace.CreateCustomRoleInput{Name: " operador de ATENDIMENTO "})
	if !errors.Is(err, workspace.ErrRoleNameTaken) || store.created != 0 {
		t.Fatalf("err = %v, created = %d", err, store.created)
	}
}

func TestRenamingARoleOntoAnotherRolesNameIsRefused(t *testing.T) {
	store := existingRoles()
	name := "gerente"
	_, err := NewUpdateCustomRoleUseCase(ownerRepo{}, store).Execute("u1", "ws", "", "r1", workspace.UpdateCustomRoleInput{Name: &name})
	if !errors.Is(err, workspace.ErrRoleNameTaken) || store.updated != 0 {
		t.Fatalf("err = %v, updated = %d", err, store.updated)
	}
}

func TestKeepingTheSameNameOnEditIsAllowed(t *testing.T) {
	store := existingRoles()
	name := "Operador de atendimento"
	if _, err := NewUpdateCustomRoleUseCase(ownerRepo{}, store).Execute("u1", "ws", "", "r1", workspace.UpdateCustomRoleInput{Name: &name}); err != nil {
		t.Fatal(err)
	}
}
