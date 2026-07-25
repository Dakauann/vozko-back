package workspace_template_access_repository

import (
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/shared"
	"vozko/domain/workspace_template_access"
	"vozko/infra/database/schema"
)

type WorkspaceTemplateAccessRepositoryImpl struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) workspace_template_access.Repository {
	return &WorkspaceTemplateAccessRepositoryImpl{db: db}
}

func (r *WorkspaceTemplateAccessRepositoryImpl) Create(access *workspace_template_access.WorkspaceTemplateAccess) error {
	existing, _ := r.FindByWorkspaceAndTemplate(access.WorkspaceID, access.TemplateID)
	if existing != nil {
		return workspace_template_access.ErrAccessAlreadyExists
	}

	dbAccess := &schema.WorkspaceTemplateAccess{
		ID:          access.ID,
		WorkspaceID: access.WorkspaceID,
		TemplateID:  access.TemplateID,
		GrantedBy:   access.GrantedBy,
	}

	if err := r.db.Create(dbAccess).Error; err != nil {
		return err
	}

	access.CreatedAt = dbAccess.CreatedAt
	return nil
}

func (r *WorkspaceTemplateAccessRepositoryImpl) Delete(id string) error {
	result := r.db.Where("id = ?", id).Delete(&schema.WorkspaceTemplateAccess{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workspace_template_access.ErrAccessNotFound
	}
	return nil
}

func (r *WorkspaceTemplateAccessRepositoryImpl) DeleteByWorkspaceAndTemplate(workspaceID, templateID string) error {
	result := r.db.Where("workspace_id = ? AND template_id = ?", workspaceID, templateID).Delete(&schema.WorkspaceTemplateAccess{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workspace_template_access.ErrAccessNotFound
	}
	return nil
}

func (r *WorkspaceTemplateAccessRepositoryImpl) FindByID(id string) (*workspace_template_access.WorkspaceTemplateAccess, error) {
	var dbAccess schema.WorkspaceTemplateAccess
	if err := r.db.Where("id = ?", id).First(&dbAccess).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workspace_template_access.ErrAccessNotFound
		}
		return nil, err
	}
	return mapToDomain(&dbAccess), nil
}

func (r *WorkspaceTemplateAccessRepositoryImpl) FindByWorkspaceAndTemplate(workspaceID, templateID string) (*workspace_template_access.WorkspaceTemplateAccess, error) {
	var dbAccess schema.WorkspaceTemplateAccess
	if err := r.db.Where("workspace_id = ? AND template_id = ?", workspaceID, templateID).First(&dbAccess).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workspace_template_access.ErrAccessNotFound
		}
		return nil, err
	}
	return mapToDomain(&dbAccess), nil
}

func (r *WorkspaceTemplateAccessRepositoryImpl) ListByWorkspace(input workspace_template_access.ListWorkspaceAccessInput) (*shared.PaginatedResult[*workspace_template_access.WorkspaceTemplateAccess], error) {
	pagination := shared.NormalizePagination(input.Pagination)
	query := r.db.Model(&schema.WorkspaceTemplateAccess{}).Where("workspace_id = ?", input.WorkspaceID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	var dbAccesses []schema.WorkspaceTemplateAccess
	if err := query.
		Order("created_at DESC").
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Find(&dbAccesses).Error; err != nil {
		return nil, err
	}

	accesses := make([]*workspace_template_access.WorkspaceTemplateAccess, len(dbAccesses))
	for i := range dbAccesses {
		accesses[i] = mapToDomain(&dbAccesses[i])
	}

	return shared.NewPaginatedResult(accesses, pagination, total), nil
}

func (r *WorkspaceTemplateAccessRepositoryImpl) ListByTemplate(input workspace_template_access.ListTemplateAccessInput) (*shared.PaginatedResult[*workspace_template_access.WorkspaceTemplateAccess], error) {
	pagination := shared.NormalizePagination(input.Pagination)
	query := r.db.Model(&schema.WorkspaceTemplateAccess{}).Where("template_id = ?", input.TemplateID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	var dbAccesses []schema.WorkspaceTemplateAccess
	if err := query.
		Order("created_at DESC").
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Find(&dbAccesses).Error; err != nil {
		return nil, err
	}

	accesses := make([]*workspace_template_access.WorkspaceTemplateAccess, len(dbAccesses))
	for i := range dbAccesses {
		accesses[i] = mapToDomain(&dbAccesses[i])
	}

	return shared.NewPaginatedResult(accesses, pagination, total), nil
}

func (r *WorkspaceTemplateAccessRepositoryImpl) GetTemplateIDsForWorkspace(workspaceID string) ([]string, error) {
	var templateIDs []string
	if err := r.db.Model(&schema.WorkspaceTemplateAccess{}).
		Where("workspace_id = ?", workspaceID).
		Pluck("template_id", &templateIDs).Error; err != nil {
		return nil, err
	}
	return templateIDs, nil
}

func (r *WorkspaceTemplateAccessRepositoryImpl) HasAccess(workspaceID, templateID string) (bool, error) {
	var count int64
	if err := r.db.Model(&schema.WorkspaceTemplateAccess{}).
		Where("workspace_id = ? AND template_id = ?", workspaceID, templateID).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *WorkspaceTemplateAccessRepositoryImpl) GrantAccess(workspaceID, templateID, grantedBy string) (*workspace_template_access.WorkspaceTemplateAccess, error) {
	access := workspace_template_access.NewWorkspaceTemplateAccess(
		uuid.New().String(),
		workspaceID,
		templateID,
		grantedBy,
	)

	if err := r.Create(access); err != nil {
		return nil, err
	}

	return access, nil
}

func mapToDomain(db *schema.WorkspaceTemplateAccess) *workspace_template_access.WorkspaceTemplateAccess {
	return &workspace_template_access.WorkspaceTemplateAccess{
		ID:          db.ID,
		WorkspaceID: db.WorkspaceID,
		TemplateID:  db.TemplateID,
		GrantedBy:   db.GrantedBy,
		CreatedAt:   db.CreatedAt,
	}
}
