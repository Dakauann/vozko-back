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

type documentRepository struct {
	db *gorm.DB
}

func NewDocumentRepository(db *gorm.DB) rag.DocumentRepository {
	return &documentRepository{db: db}
}

func (r *documentRepository) Create(ctx context.Context, doc *rag.Document) error {
	schemaDoc := mapDocumentToSchema(doc)
	return r.db.WithContext(ctx).Create(schemaDoc).Error
}

func (r *documentRepository) Update(ctx context.Context, doc *rag.Document) error {
	metadataJSON, _ := json.Marshal(doc.Metadata)

	var mediaID *string
	if doc.MediaID != "" {
		mediaID = &doc.MediaID
	}

	update := map[string]interface{}{
		"name":          doc.Name,
		"type":          string(doc.Type),
		"status":        string(doc.Status),
		"content":       doc.Content,
		"media_id":      mediaID,
		"size_bytes":    doc.SizeBytes,
		"chunk_count":   doc.ChunkCount,
		"metadata":      string(metadataJSON),
		"error_message": doc.ErrorMessage,
	}

	result := r.db.WithContext(ctx).Model(&schema.RAGDocument{}).Where("id = ?", doc.ID).Updates(update)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return rag.ErrDocumentNotFound
	}
	return nil
}

func (r *documentRepository) Delete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Delete(&schema.RAGDocument{}, "id = ?", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return rag.ErrDocumentNotFound
	}
	return nil
}

func (r *documentRepository) FindByID(ctx context.Context, id string) (*rag.Document, error) {
	var schemaDoc schema.RAGDocument
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&schemaDoc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, rag.ErrDocumentNotFound
		}
		return nil, err
	}
	return mapDocumentToDomain(&schemaDoc), nil
}

func (r *documentRepository) FindByIDs(ctx context.Context, ids []string) ([]*rag.Document, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var items []schema.RAGDocument
	if err := r.db.WithContext(ctx).Where("id IN (?)", ids).Find(&items).Error; err != nil {
		return nil, err
	}
	results := make([]*rag.Document, 0, len(items))
	for i := range items {
		results = append(results, mapDocumentToDomain(&items[i]))
	}
	return results, nil
}

func (r *documentRepository) FindByKnowledgeBase(ctx context.Context, knowledgeBaseID string, opts shared.Pagination) (*shared.PaginatedResult[*rag.Document], error) {
	pagination := shared.NormalizePagination(opts)

	var total int64
	if err := r.db.WithContext(ctx).Model(&schema.RAGDocument{}).Where("knowledge_base_id = ?", knowledgeBaseID).Count(&total).Error; err != nil {
		return nil, err
	}

	var items []schema.RAGDocument
	if err := r.db.WithContext(ctx).
		Where("knowledge_base_id = ?", knowledgeBaseID).
		Order("created_at DESC").
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Find(&items).Error; err != nil {
		return nil, err
	}

	results := make([]*rag.Document, 0, len(items))
	for i := range items {
		results = append(results, mapDocumentToDomain(&items[i]))
	}

	return shared.NewPaginatedResult(results, pagination, total), nil
}

func (r *documentRepository) FindPending(ctx context.Context, limit int) ([]*rag.Document, error) {
	var items []schema.RAGDocument
	if err := r.db.WithContext(ctx).
		Where("status = ?", string(rag.DocumentStatusPending)).
		Order("created_at ASC").
		Limit(limit).
		Find(&items).Error; err != nil {
		return nil, err
	}

	results := make([]*rag.Document, 0, len(items))
	for i := range items {
		results = append(results, mapDocumentToDomain(&items[i]))
	}

	return results, nil
}

func (r *documentRepository) CountByKnowledgeBase(ctx context.Context, knowledgeBaseID string) (int, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&schema.RAGDocument{}).Where("knowledge_base_id = ?", knowledgeBaseID).Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}

func (r *documentRepository) UpdateStatus(ctx context.Context, id string, status rag.DocumentStatus, errMsg string) error {
	return r.db.WithContext(ctx).Model(&schema.RAGDocument{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":        string(status),
			"error_message": errMsg,
		}).Error
}

func mapDocumentToSchema(doc *rag.Document) *schema.RAGDocument {
	metadataJSON, _ := json.Marshal(doc.Metadata)

	var mediaID *string
	if doc.MediaID != "" {
		mediaID = &doc.MediaID
	}

	return &schema.RAGDocument{
		ID:              doc.ID,
		KnowledgeBaseID: doc.KnowledgeBaseID,
		Name:            doc.Name,
		Type:            string(doc.Type),
		Status:          string(doc.Status),
		Content:         doc.Content,
		MediaID:         mediaID,
		SizeBytes:       doc.SizeBytes,
		ChunkCount:      doc.ChunkCount,
		Metadata:        string(metadataJSON),
		ErrorMessage:    doc.ErrorMessage,
		CreatedAt:       doc.CreatedAt,
		UpdatedAt:       doc.UpdatedAt,
	}
}

func mapDocumentToDomain(s *schema.RAGDocument) *rag.Document {
	var metadata map[string]string
	if s.Metadata != "" {
		_ = json.Unmarshal([]byte(s.Metadata), &metadata)
	}
	if metadata == nil {
		metadata = make(map[string]string)
	}

	var mediaID string
	if s.MediaID != nil {
		mediaID = *s.MediaID
	}

	return &rag.Document{
		ID:              s.ID,
		KnowledgeBaseID: s.KnowledgeBaseID,
		Name:            s.Name,
		Type:            rag.DocumentType(s.Type),
		Status:          rag.DocumentStatus(s.Status),
		Content:         s.Content,
		MediaID:         mediaID,
		SizeBytes:       s.SizeBytes,
		ChunkCount:      s.ChunkCount,
		Metadata:        metadata,
		ErrorMessage:    s.ErrorMessage,
		CreatedAt:       s.CreatedAt,
		UpdatedAt:       s.UpdatedAt,
	}
}
