package workspace_repository

import (
	"encoding/json"
	"errors"

	"gorm.io/gorm"

	"vozko/domain/workspace"
	"vozko/infra/database/schema"
)

type customRoleRepository struct {
	db *gorm.DB
}

func NewCustomRoleRepository(db *gorm.DB) workspace.CustomRoleRepository {
	return &customRoleRepository{db: db}
}

func (r *customRoleRepository) CreateRole(role *workspace.CustomRole) error {
	permJSON, err := json.Marshal(role.Permissions)
	if err != nil {
		return err
	}
	dbRole := schema.WorkspaceCustomRole{
		ID:          role.ID,
		WorkspaceID: role.WorkspaceID,
		Name:        role.Name,
		Description: role.Description,
		Permissions: string(permJSON),
	}
	if err := r.db.Create(&dbRole).Error; err != nil {
		return err
	}
	role.CreatedAt = dbRole.CreatedAt
	role.UpdatedAt = dbRole.UpdatedAt
	return nil
}

func (r *customRoleRepository) GetRoleByID(id string) (*workspace.CustomRole, error) {
	var dbRole schema.WorkspaceCustomRole
	if err := r.db.Where("id = ?", id).First(&dbRole).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workspace.ErrRoleNotFound
		}
		return nil, err
	}
	return mapRoleToDomain(&dbRole), nil
}

func (r *customRoleRepository) ListRolesByWorkspace(workspaceID string) ([]*workspace.CustomRole, error) {
	var dbRoles []schema.WorkspaceCustomRole
	if err := r.db.Where("workspace_id = ?", workspaceID).Order("name ASC").Find(&dbRoles).Error; err != nil {
		return nil, err
	}
	result := make([]*workspace.CustomRole, len(dbRoles))
	for i := range dbRoles {
		result[i] = mapRoleToDomain(&dbRoles[i])
	}
	return result, nil
}

func (r *customRoleRepository) UpdateRole(role *workspace.CustomRole) error {
	updates := map[string]interface{}{
		"name":        role.Name,
		"description": role.Description,
	}
	if role.Permissions != nil {
		permJSON, err := json.Marshal(role.Permissions)
		if err != nil {
			return err
		}
		updates["permissions"] = string(permJSON)
	}
	return r.db.Model(&schema.WorkspaceCustomRole{}).Where("id = ?", role.ID).Updates(updates).Error
}

func (r *customRoleRepository) DeleteRole(id string) error {
	result := r.db.Delete(&schema.WorkspaceCustomRole{}, "id = ?", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workspace.ErrRoleNotFound
	}
	return nil
}

func (r *customRoleRepository) ListMembersByRoleID(roleID string) ([]*workspace.Member, error) {
	var dbMembers []schema.WorkspaceMember
	if err := r.db.Preload("User").Preload("CustomRole").Where("role_id = ?", roleID).Find(&dbMembers).Error; err != nil {
		return nil, err
	}
	result := make([]*workspace.Member, len(dbMembers))
	for i := range dbMembers {
		result[i] = mapMemberToDomain(&dbMembers[i])
	}
	return result, nil
}

func mapRoleToDomain(dbRole *schema.WorkspaceCustomRole) *workspace.CustomRole {
	cr := &workspace.CustomRole{
		ID:          dbRole.ID,
		WorkspaceID: dbRole.WorkspaceID,
		Name:        dbRole.Name,
		Description: dbRole.Description,
		CreatedAt:   dbRole.CreatedAt,
		UpdatedAt:   dbRole.UpdatedAt,
	}
	if dbRole.Permissions != "" {
		_ = json.Unmarshal([]byte(dbRole.Permissions), &cr.Permissions)
	}
	if cr.Permissions == nil {
		cr.Permissions = []workspace.PermissionEntry{}
	}
	return cr
}
