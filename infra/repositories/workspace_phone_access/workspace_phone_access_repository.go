package workspace_phone_access_repository

import (
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/shared"
	"vozko/domain/workspace_phone_access"
	"vozko/infra/database/schema"
)

type WorkspacePhoneAccessRepositoryImpl struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) workspace_phone_access.Repository {
	return &WorkspacePhoneAccessRepositoryImpl{db: db}
}

func (r *WorkspacePhoneAccessRepositoryImpl) Create(access *workspace_phone_access.WorkspacePhoneAccess) error {
	existing, _ := r.FindByWorkspaceAndPhone(access.WorkspaceID, access.PhoneID)
	if existing != nil {
		return workspace_phone_access.ErrAccessAlreadyExists
	}

	if access.ID == "" {
		access.ID = uuid.New().String()
	}

	dbAccess := &schema.WorkspacePhoneAccess{
		ID:          access.ID,
		WorkspaceID: access.WorkspaceID,
		PhoneID:     access.PhoneID,
		GrantedBy:   access.GrantedBy,
	}

	if err := r.db.Create(dbAccess).Error; err != nil {
		return err
	}

	access.CreatedAt = dbAccess.CreatedAt
	return nil
}

func (r *WorkspacePhoneAccessRepositoryImpl) Delete(id string) error {
	result := r.db.Where("id = ?", id).Delete(&schema.WorkspacePhoneAccess{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workspace_phone_access.ErrAccessNotFound
	}
	return nil
}

func (r *WorkspacePhoneAccessRepositoryImpl) DeleteByWorkspaceAndPhone(workspaceID, phoneID string) error {
	result := r.db.Where("workspace_id = ? AND phone_id = ?", workspaceID, phoneID).Delete(&schema.WorkspacePhoneAccess{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workspace_phone_access.ErrAccessNotFound
	}
	return nil
}

func (r *WorkspacePhoneAccessRepositoryImpl) FindByID(id string) (*workspace_phone_access.WorkspacePhoneAccess, error) {
	var dbAccess schema.WorkspacePhoneAccess
	if err := r.db.Where("id = ?", id).First(&dbAccess).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workspace_phone_access.ErrAccessNotFound
		}
		return nil, err
	}
	return mapToDomain(&dbAccess), nil
}

func (r *WorkspacePhoneAccessRepositoryImpl) FindByWorkspaceAndPhone(workspaceID, phoneID string) (*workspace_phone_access.WorkspacePhoneAccess, error) {
	var dbAccess schema.WorkspacePhoneAccess
	if err := r.db.Where("workspace_id = ? AND phone_id = ?", workspaceID, phoneID).First(&dbAccess).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workspace_phone_access.ErrAccessNotFound
		}
		return nil, err
	}
	return mapToDomain(&dbAccess), nil
}

func (r *WorkspacePhoneAccessRepositoryImpl) ListByWorkspace(input workspace_phone_access.ListWorkspaceAccessInput) (*shared.PaginatedResult[*workspace_phone_access.WorkspacePhoneAccess], error) {
	pagination := shared.NormalizePagination(input.Pagination)
	query := r.db.Model(&schema.WorkspacePhoneAccess{}).Where("workspace_id = ?", input.WorkspaceID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	var dbAccesses []schema.WorkspacePhoneAccess
	if err := query.
		Order("created_at DESC").
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Find(&dbAccesses).Error; err != nil {
		return nil, err
	}

	accesses := make([]*workspace_phone_access.WorkspacePhoneAccess, len(dbAccesses))
	for i := range dbAccesses {
		accesses[i] = mapToDomain(&dbAccesses[i])
	}

	return shared.NewPaginatedResult(accesses, pagination, total), nil
}

func (r *WorkspacePhoneAccessRepositoryImpl) ListByPhone(input workspace_phone_access.ListPhoneAccessInput) (*shared.PaginatedResult[*workspace_phone_access.WorkspacePhoneAccess], error) {
	pagination := shared.NormalizePagination(input.Pagination)
	query := r.db.Model(&schema.WorkspacePhoneAccess{}).Where("phone_id = ?", input.PhoneID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	var dbAccesses []schema.WorkspacePhoneAccess
	if err := query.
		Order("created_at DESC").
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Find(&dbAccesses).Error; err != nil {
		return nil, err
	}

	accesses := make([]*workspace_phone_access.WorkspacePhoneAccess, len(dbAccesses))
	for i := range dbAccesses {
		accesses[i] = mapToDomain(&dbAccesses[i])
	}

	return shared.NewPaginatedResult(accesses, pagination, total), nil
}

func (r *WorkspacePhoneAccessRepositoryImpl) GetPhoneIDsForWorkspace(workspaceID string) ([]string, error) {
	var phoneIDs []string
	if err := r.db.Model(&schema.WorkspacePhoneAccess{}).
		Where("workspace_id = ?", workspaceID).
		Pluck("phone_id", &phoneIDs).Error; err != nil {
		return nil, err
	}
	return phoneIDs, nil
}

func (r *WorkspacePhoneAccessRepositoryImpl) HasAccess(workspaceID, phoneID string) (bool, error) {
	var count int64
	if err := r.db.Model(&schema.WorkspacePhoneAccess{}).
		Where("workspace_id = ? AND phone_id = ?", workspaceID, phoneID).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func mapToDomain(db *schema.WorkspacePhoneAccess) *workspace_phone_access.WorkspacePhoneAccess {
	return &workspace_phone_access.WorkspacePhoneAccess{
		ID:          db.ID,
		WorkspaceID: db.WorkspaceID,
		PhoneID:     db.PhoneID,
		GrantedBy:   db.GrantedBy,
		CreatedAt:   db.CreatedAt,
	}
}
