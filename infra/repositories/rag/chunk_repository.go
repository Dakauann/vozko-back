package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"vozko/domain/rag"
	"vozko/infra/database/schema"

	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type chunkRepository struct {
	db *gorm.DB
}

func NewChunkRepository(db *gorm.DB) rag.ChunkRepository {
	return &chunkRepository{db: db}
}

func (r *chunkRepository) CreateBatch(ctx context.Context, chunks []*rag.Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	schemaChunks := make([]schema.RAGChunk, 0, len(chunks))
	for _, chunk := range chunks {
		schemaChunks = append(schemaChunks, *mapChunkToSchema(chunk))
	}

	err := r.db.WithContext(ctx).Session(&gorm.Session{
		Logger: r.db.Logger.LogMode(logger.Silent),
	}).CreateInBatches(schemaChunks, 100).Error
	if err != nil {
		return fmt.Errorf("failed to insert %d chunks: %w", len(schemaChunks), err)
	}
	return nil
}

func (r *chunkRepository) DeleteByDocument(ctx context.Context, documentID string) error {
	return r.db.WithContext(ctx).Where("document_id = ?", documentID).Delete(&schema.RAGChunk{}).Error
}

func (r *chunkRepository) DeleteByKnowledgeBase(ctx context.Context, knowledgeBaseID string) error {
	return r.db.WithContext(ctx).Where("knowledge_base_id = ?", knowledgeBaseID).Delete(&schema.RAGChunk{}).Error
}

func (r *chunkRepository) FindByDocument(ctx context.Context, documentID string) ([]*rag.Chunk, error) {
	var items []schema.RAGChunk
	if err := r.db.WithContext(ctx).
		Where("document_id = ?", documentID).
		Order("index ASC").
		Find(&items).Error; err != nil {
		return nil, err
	}

	results := make([]*rag.Chunk, 0, len(items))
	for i := range items {
		results = append(results, mapChunkToDomain(&items[i]))
	}

	return results, nil
}

func (r *chunkRepository) CountByKnowledgeBase(ctx context.Context, knowledgeBaseID string) (int, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&schema.RAGChunk{}).Where("knowledge_base_id = ?", knowledgeBaseID).Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}

func (r *chunkRepository) FindByDocumentAndIndexRange(ctx context.Context, documentID string, minIndex int, maxIndex int) ([]*rag.Chunk, error) {
	var items []schema.RAGChunk
	if err := r.db.WithContext(ctx).
		Select("id, document_id, knowledge_base_id, content, \"index\", start_offset, end_offset, metadata, token_count, created_at").
		Where("document_id = ? AND \"index\" >= ? AND \"index\" <= ?", documentID, minIndex, maxIndex).
		Order("\"index\" ASC").
		Find(&items).Error; err != nil {
		return nil, err
	}

	results := make([]*rag.Chunk, 0, len(items))
	for i := range items {
		results = append(results, mapChunkToDomain(&items[i]))
	}
	return results, nil
}

func (r *chunkRepository) FindNeighborsBatch(ctx context.Context, requests []rag.ChunkNeighborRequest) ([]*rag.Chunk, error) {
	if len(requests) == 0 {
		return nil, nil
	}

	docIndices := make(map[string]map[int]bool)
	for _, req := range requests {
		if _, ok := docIndices[req.DocumentID]; !ok {
			docIndices[req.DocumentID] = make(map[int]bool)
		}
		for idx := req.MinIndex; idx <= req.MaxIndex; idx++ {
			docIndices[req.DocumentID][idx] = true
		}
	}

	var conditions []string
	var args []interface{}
	for docID, indices := range docIndices {
		args = append(args, docID)
		placeholders := make([]string, 0, len(indices))
		for idx := range indices {
			placeholders = append(placeholders, "?")
			args = append(args, idx)
		}
		conditions = append(conditions, fmt.Sprintf("(document_id = ? AND \"index\" IN (%s))", strings.Join(placeholders, ",")))
	}

	query := fmt.Sprintf(`SELECT id, document_id, knowledge_base_id, content, "index",
		start_offset, end_offset, metadata, token_count, created_at
		FROM rag_chunks WHERE %s ORDER BY document_id, "index"`, strings.Join(conditions, " OR "))

	var items []schema.RAGChunk
	if err := r.db.WithContext(ctx).Raw(query, args...).Scan(&items).Error; err != nil {
		return nil, fmt.Errorf("batch neighbor fetch failed: %w", err)
	}

	results := make([]*rag.Chunk, 0, len(items))
	for i := range items {
		results = append(results, mapChunkToDomain(&items[i]))
	}
	return results, nil
}

func mapChunkToSchema(chunk *rag.Chunk) *schema.RAGChunk {
	metadataJSON, _ := json.Marshal(chunk.Metadata)

	var embedding pgvector.Vector
	if len(chunk.Embedding) > 0 {
		embedding = pgvector.NewVector(chunk.Embedding)
	}

	content := strings.ToValidUTF8(chunk.Content, "")

	return &schema.RAGChunk{
		ID:              chunk.ID,
		DocumentID:      chunk.DocumentID,
		KnowledgeBaseID: chunk.KnowledgeBaseID,
		Content:         content,
		Embedding:       embedding,
		Index:           chunk.Index,
		StartOffset:     chunk.StartOffset,
		EndOffset:       chunk.EndOffset,
		Metadata:        string(metadataJSON),
		TokenCount:      chunk.TokenCount,
		CreatedAt:       chunk.CreatedAt,
	}
}

func mapChunkToDomain(s *schema.RAGChunk) *rag.Chunk {
	var metadata map[string]string
	if s.Metadata != "" {
		_ = json.Unmarshal([]byte(s.Metadata), &metadata)
	}
	if metadata == nil {
		metadata = make(map[string]string)
	}

	return &rag.Chunk{
		ID:              s.ID,
		DocumentID:      s.DocumentID,
		KnowledgeBaseID: s.KnowledgeBaseID,
		Content:         s.Content,
		Embedding:       s.Embedding.Slice(),
		Index:           s.Index,
		StartOffset:     s.StartOffset,
		EndOffset:       s.EndOffset,
		Metadata:        metadata,
		TokenCount:      s.TokenCount,
		CreatedAt:       s.CreatedAt,
	}
}
