package workspace_department_repository

import (
	"errors"

	"gorm.io/gorm"

	workspace_department "vozko/domain/workspace/workspace_department"
	"vozko/infra/database/schema"
)

type repositoryImpl struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) workspace_department.Repository {
	return &repositoryImpl{db: db}
}

func (r *repositoryImpl) CreateDepartment(dept *workspace_department.Department) error {
	row := schema.WorkspaceDepartment{
		ID:          dept.ID,
		WorkspaceID: dept.WorkspaceID,
		Name:        dept.Name,
		Description: dept.Description,
	}
	if err := r.db.Create(&row).Error; err != nil {
		return err
	}
	dept.CreatedAt = row.CreatedAt
	dept.UpdatedAt = row.UpdatedAt
	return nil
}

func (r *repositoryImpl) GetDepartmentByID(id string) (*workspace_department.Department, error) {
	var row schema.WorkspaceDepartment
	if err := r.db.Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workspace_department.ErrDepartmentNotFound
		}
		return nil, err
	}
	return mapDepartment(row), nil
}

func (r *repositoryImpl) ListDepartments(workspaceID string) ([]workspace_department.Department, error) {
	var rows []schema.WorkspaceDepartment
	if err := r.db.Where("workspace_id = ?", workspaceID).
		Order("created_at ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return r.mapWithMemberCounts(rows)
}

func (r *repositoryImpl) ListDepartmentsByIDs(ids []string) ([]workspace_department.Department, error) {
	if len(ids) == 0 {
		return []workspace_department.Department{}, nil
	}
	var rows []schema.WorkspaceDepartment
	if err := r.db.Where("id IN ?", ids).
		Order("created_at ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return r.mapWithMemberCounts(rows)
}

func (r *repositoryImpl) UpdateDepartment(dept *workspace_department.Department) error {
	res := r.db.Model(&schema.WorkspaceDepartment{}).
		Where("id = ?", dept.ID).
		Updates(map[string]interface{}{
			"name":        dept.Name,
			"description": dept.Description,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return workspace_department.ErrDepartmentNotFound
	}
	return nil
}

func (r *repositoryImpl) DeleteDepartment(id string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("department_id = ?", id).
			Delete(&schema.WorkspaceDepartmentMember{}).Error; err != nil {
			return err
		}
		res := tx.Where("id = ?", id).Delete(&schema.WorkspaceDepartment{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return workspace_department.ErrDepartmentNotFound
		}
		return nil
	})
}

func (r *repositoryImpl) AddMember(dm *workspace_department.DepartmentMember) error {
	row := schema.WorkspaceDepartmentMember{
		ID:           dm.ID,
		DepartmentID: dm.DepartmentID,
		MemberID:     dm.MemberID,
	}
	if err := r.db.Create(&row).Error; err != nil {
		if isDuplicateKeyError(err) {
			return workspace_department.ErrDepartmentMemberExists
		}
		return err
	}
	dm.CreatedAt = row.CreatedAt
	return nil
}

func (r *repositoryImpl) RemoveMember(departmentID, memberID string) error {
	res := r.db.Where("department_id = ? AND member_id = ?", departmentID, memberID).
		Delete(&schema.WorkspaceDepartmentMember{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return workspace_department.ErrDepartmentMemberNotFound
	}
	return nil
}

func (r *repositoryImpl) ListMembers(departmentID string) ([]workspace_department.DepartmentMember, error) {
	var rows []struct {
		schema.WorkspaceDepartmentMember
		UserID   string `gorm:"column:user_id"`
		Email    string `gorm:"column:email"`
		Username string `gorm:"column:username"`
		Role     string `gorm:"column:role"`
	}

	if err := r.db.Table("workspace_department_members dm").
		Select("dm.*, wm.user_id, u.email, u.username, wm.role").
		Joins("JOIN workspace_members wm ON wm.id = dm.member_id").
		Joins("JOIN users u ON u.id = wm.user_id").
		Where("dm.department_id = ?", departmentID).
		Order("dm.created_at ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	result := make([]workspace_department.DepartmentMember, len(rows))
	for i, row := range rows {
		result[i] = workspace_department.DepartmentMember{
			ID:           row.ID,
			DepartmentID: row.DepartmentID,
			MemberID:     row.MemberID,
			UserID:       row.UserID,
			Email:        row.Email,
			Username:     row.Username,
			Role:         row.Role,
			CreatedAt:    row.CreatedAt,
		}
	}
	return result, nil
}

func (r *repositoryImpl) GetMemberDepartmentIDs(workspaceID, userID string) ([]string, error) {
	var ids []string
	if err := r.db.Table("workspace_department_members dm").
		Select("dm.department_id").
		Joins("JOIN workspace_departments d ON d.id = dm.department_id AND d.deleted_at IS NULL").
		Joins("JOIN workspace_members wm ON wm.id = dm.member_id").
		Where("d.workspace_id = ? AND wm.user_id = ?", workspaceID, userID).
		Pluck("dm.department_id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *repositoryImpl) IsMember(departmentID, memberID string) (bool, error) {
	var count int64
	if err := r.db.Model(&schema.WorkspaceDepartmentMember{}).
		Where("department_id = ? AND member_id = ?", departmentID, memberID).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// mapWithMemberCounts maps department rows to domain objects and fills in each
// department's live member count with a single grouped query, so listings don't
// report "no members" for departments that actually have members.
func (r *repositoryImpl) mapWithMemberCounts(rows []schema.WorkspaceDepartment) ([]workspace_department.Department, error) {
	result := make([]workspace_department.Department, len(rows))
	ids := make([]string, len(rows))
	for i, row := range rows {
		result[i] = *mapDepartment(row)
		ids[i] = row.ID
	}

	counts, err := r.memberCounts(ids)
	if err != nil {
		return nil, err
	}
	for i := range result {
		result[i].MemberCount = counts[result[i].ID]
	}
	return result, nil
}

// memberCounts returns the number of members per department for the given
// department IDs, keyed by department ID.
func (r *repositoryImpl) memberCounts(departmentIDs []string) (map[string]int, error) {
	counts := make(map[string]int, len(departmentIDs))
	if len(departmentIDs) == 0 {
		return counts, nil
	}

	var rows []struct {
		DepartmentID string `gorm:"column:department_id"`
		Count        int    `gorm:"column:count"`
	}
	if err := r.db.Model(&schema.WorkspaceDepartmentMember{}).
		Select("department_id, COUNT(*) AS count").
		Where("department_id IN ?", departmentIDs).
		Group("department_id").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		counts[row.DepartmentID] = row.Count
	}
	return counts, nil
}

func mapDepartment(row schema.WorkspaceDepartment) *workspace_department.Department {
	return &workspace_department.Department{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		Name:        row.Name,
		Description: row.Description,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}

func isDuplicateKeyError(err error) bool {
	return err != nil && (errors.As(err, &duplicateKeyErr{}) ||
		containsDuplicateKey(err.Error()))
}

type duplicateKeyErr struct{}

func (duplicateKeyErr) Error() string { return "duplicate key" }

func containsDuplicateKey(s string) bool {
	return len(s) > 0 && (contains(s, "duplicate key") || contains(s, "UNIQUE constraint"))
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
