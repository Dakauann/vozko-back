package rag

import (
	"context"
	"encoding/json"
	"errors"

	"vozko/domain/rag"
	"vozko/domain/shared"
	"vozko/infra/database/schema"

	"gorm.io/gorm"
)

type knowledgeBaseRepository struct {
	db *gorm.DB
}

func NewKnowledgeBaseRepository(db *gorm.DB) rag.KnowledgeBaseRepository {
	return &knowledgeBaseRepository{db: db}
}

func (r *knowledgeBaseRepository) Create(ctx context.Context, kb *rag.KnowledgeBase) error {
	schemaKB := mapKnowledgeBaseToSchema(kb)
	return r.db.WithContext(ctx).Create(schemaKB).Error
}

func (r *knowledgeBaseRepository) Update(ctx context.Context, kb *rag.KnowledgeBase) error {
	configJSON, _ := json.Marshal(kb.Config)

	update := map[string]interface{}{
		"name":           kb.Name,
		"description":    kb.Description,
		"status":         string(kb.Status),
		"config":         string(configJSON),
		"document_count": kb.DocumentCount,
		"chunk_count":    kb.ChunkCount,
		"total_size_mb":  kb.TotalSizeMB,
	}

	result := r.db.WithContext(ctx).Model(&schema.KnowledgeBase{}).Where("id = ?", kb.ID).Updates(update)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return rag.ErrKnowledgeBaseNotFound
	}
	return nil
}

func (r *knowledgeBaseRepository) Delete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Delete(&schema.KnowledgeBase{}, "id = ?", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return rag.ErrKnowledgeBaseNotFound
	}
	return nil
}

func (r *knowledgeBaseRepository) FindByID(ctx context.Context, id string) (*rag.KnowledgeBase, error) {
	var schemaKB schema.KnowledgeBase
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&schemaKB).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, rag.ErrKnowledgeBaseNotFound
		}
		return nil, err
	}
	return mapKnowledgeBaseToDomain(&schemaKB), nil
}

func (r *knowledgeBaseRepository) FindByWorkspaceAndDepartment(ctx context.Context, workspaceID string, departmentID *string, opts shared.Pagination) (*shared.PaginatedResult[*rag.KnowledgeBase], error) {
	pagination := shared.NormalizePagination(opts)
	var total int64
	query := r.db.WithContext(ctx).Model(&schema.KnowledgeBase{}).Where("workspace_id = ?", workspaceID)
	if departmentID != nil {
		query = query.Where("department_id = ?", *departmentID)
	} else {
		query = query.Where("department_id IS NULL")
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	var items []schema.KnowledgeBase
	query = r.db.WithContext(ctx).
		Where("workspace_id = ?", workspaceID)
	if departmentID != nil {
		query = query.Where("department_id = ?", *departmentID)
	} else {
		query = query.Where("department_id IS NULL")
	}
	query = query.Order("created_at DESC").
		Offset(pagination.Offset()).
		Limit(pagination.PageSize)
	if err := query.Find(&items).Error; err != nil {
		return nil, err
	}
	results := make([]*rag.KnowledgeBase, 0, len(items))
	for i := range items {
		results = append(results, mapKnowledgeBaseToDomain(&items[i]))
	}
	return shared.NewPaginatedResult(results, pagination, total), nil
}

func (r *knowledgeBaseRepository) FindByWorkspace(ctx context.Context, workspaceID string, opts shared.Pagination) (*shared.PaginatedResult[*rag.KnowledgeBase], error) {
	pagination := shared.NormalizePagination(opts)

	var total int64
	if err := r.db.WithContext(ctx).Model(&schema.KnowledgeBase{}).Where("workspace_id = ?", workspaceID).Count(&total).Error; err != nil {
		return nil, err
	}

	var items []schema.KnowledgeBase
	if err := r.db.WithContext(ctx).
		Where("workspace_id = ?", workspaceID).
		Order("created_at DESC").
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Find(&items).Error; err != nil {
		return nil, err
	}

	results := make([]*rag.KnowledgeBase, 0, len(items))
	for i := range items {
		results = append(results, mapKnowledgeBaseToDomain(&items[i]))
	}

	return shared.NewPaginatedResult(results, pagination, total), nil
}

func (r *knowledgeBaseRepository) FindByIDs(ctx context.Context, ids []string) ([]*rag.KnowledgeBase, error) {
	if len(ids) == 0 {
		return []*rag.KnowledgeBase{}, nil
	}

	var items []schema.KnowledgeBase
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&items).Error; err != nil {
		return nil, err
	}

	results := make([]*rag.KnowledgeBase, 0, len(items))
	for i := range items {
		results = append(results, mapKnowledgeBaseToDomain(&items[i]))
	}

	return results, nil
}

func (r *knowledgeBaseRepository) IncrementDocumentCount(ctx context.Context, id string, delta int) error {
	return r.db.WithContext(ctx).Model(&schema.KnowledgeBase{}).
		Where("id = ?", id).
		UpdateColumn("document_count", gorm.Expr("document_count + ?", delta)).Error
}

func (r *knowledgeBaseRepository) CountByWorkspace(ctx context.Context, workspaceID string) (int, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&schema.KnowledgeBase{}).Where("workspace_id = ?", workspaceID).Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}

func (r *knowledgeBaseRepository) IncrementChunkCount(ctx context.Context, id string, delta int) error {
	return r.db.WithContext(ctx).Model(&schema.KnowledgeBase{}).
		Where("id = ?", id).
		UpdateColumn("chunk_count", gorm.Expr("chunk_count + ?", delta)).Error
}

func (r *knowledgeBaseRepository) UpdateStats(ctx context.Context, id string, docCount int, chunkCount int, sizeMB float64) error {
	return r.db.WithContext(ctx).Model(&schema.KnowledgeBase{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"document_count": docCount,
			"chunk_count":    chunkCount,
			"total_size_mb":  sizeMB,
		}).Error
}

func mapKnowledgeBaseToSchema(kb *rag.KnowledgeBase) *schema.KnowledgeBase {
	configJSON, _ := json.Marshal(kb.Config)

	return &schema.KnowledgeBase{
		ID:            kb.ID,
		WorkspaceID:   kb.WorkspaceID,
		Name:          kb.Name,
		Description:   kb.Description,
		Status:        string(kb.Status),
		Config:        string(configJSON),
		DocumentCount: kb.DocumentCount,
		ChunkCount:    kb.ChunkCount,
		TotalSizeMB:   kb.TotalSizeMB,
		CreatedAt:     kb.CreatedAt,
		UpdatedAt:     kb.UpdatedAt,
	}
}

func mapKnowledgeBaseToDomain(s *schema.KnowledgeBase) *rag.KnowledgeBase {
	var config rag.KnowledgeBaseConfig
	if s.Config != "" {
		_ = json.Unmarshal([]byte(s.Config), &config)
	}
	if config.ChunkSize == 0 {
		config = rag.DefaultKnowledgeBaseConfig()
	}

	var deptID string
	if s.DepartmentID != nil {
		deptID = *s.DepartmentID
	}

	return &rag.KnowledgeBase{
		ID:            s.ID,
		WorkspaceID:   s.WorkspaceID,
		DepartmentID:  deptID,
		Name:          s.Name,
		Description:   s.Description,
		Status:        rag.KnowledgeBaseStatus(s.Status),
		Config:        config,
		DocumentCount: s.DocumentCount,
		ChunkCount:    s.ChunkCount,
		TotalSizeMB:   s.TotalSizeMB,
		CreatedAt:     s.CreatedAt,
		UpdatedAt:     s.UpdatedAt,
	}
}
