package rag

import (
	"context"
	"fmt"
	"log"
	"strings"

	"vozko/domain/rag"
	"vozko/infra/database/schema"

	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
)

type vectorRepository struct {
	db *gorm.DB
}

func NewVectorRepository(db *gorm.DB) rag.VectorRepository {
	return &vectorRepository{db: db}
}

func (r *vectorRepository) SimilaritySearch(ctx context.Context, input rag.SimilaritySearchInput) (*rag.SimilaritySearchResult, error) {
	if len(input.KnowledgeBaseIDs) == 0 {
		return &rag.SimilaritySearchResult{Chunks: []rag.ChunkWithScore{}, TotalFound: 0}, nil
	}

	if input.TopK <= 0 {
		input.TopK = 5
	}
	if input.MinScore <= 0 {
		input.MinScore = 0.0
	}

	queryVector := pgvector.NewVector(input.QueryEmbedding)

	placeholders := make([]string, len(input.KnowledgeBaseIDs))
	for i := range input.KnowledgeBaseIDs {
		placeholders[i] = "?"
	}

	query := fmt.Sprintf(`
		WITH scored AS (
			SELECT id, document_id, knowledge_base_id, content,
			       "index", start_offset, end_offset, metadata, token_count, created_at,
			       (1 - (embedding <=> ?)) AS score
			FROM rag_chunks
			WHERE knowledge_base_id IN (%s)
			  AND embedding IS NOT NULL
			ORDER BY embedding <=> ? ASC
			LIMIT ?
		)
		SELECT * FROM scored WHERE score >= ?
	`, strings.Join(placeholders, ","))

	queryArgs := []interface{}{queryVector}
	for _, id := range input.KnowledgeBaseIDs {
		queryArgs = append(queryArgs, id)
	}
	queryArgs = append(queryArgs, queryVector, input.TopK, input.MinScore)

	type chunkWithScore struct {
		schema.RAGChunk
		Score float32 `gorm:"column:score"`
	}

	var results []chunkWithScore
	if err := r.db.WithContext(ctx).Raw(query, queryArgs...).Scan(&results).Error; err != nil {
		return nil, fmt.Errorf("similarity search failed: %w", err)
	}

	log.Printf("[RAG Search] KBs=%d queryDim=%d minScore=%.2f results=%d",
		len(input.KnowledgeBaseIDs), len(input.QueryEmbedding), input.MinScore, len(results))
	if len(results) > 0 {
		log.Printf("[RAG Search] score_range=[%.4f, %.4f]", results[len(results)-1].Score, results[0].Score)
	}

	chunks := make([]rag.ChunkWithScore, 0, len(results))
	for _, r := range results {
		chunks = append(chunks, rag.ChunkWithScore{
			Chunk:      mapChunkToDomain(&r.RAGChunk),
			Score:      r.Score,
			DocumentID: r.DocumentID,
		})
	}

	return &rag.SimilaritySearchResult{
		Chunks:     chunks,
		TotalFound: len(chunks),
	}, nil
}

func (r *vectorRepository) UpsertChunks(ctx context.Context, chunks []*rag.Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	schemaChunks := make([]schema.RAGChunk, 0, len(chunks))
	for _, chunk := range chunks {
		schemaChunks = append(schemaChunks, *mapChunkToSchema(chunk))
	}

	return r.db.WithContext(ctx).Save(&schemaChunks).Error
}

func (r *vectorRepository) DeleteByDocument(ctx context.Context, documentID string) error {
	return r.db.WithContext(ctx).Where("document_id = ?", documentID).Delete(&schema.RAGChunk{}).Error
}

func (r *vectorRepository) DeleteByKnowledgeBase(ctx context.Context, knowledgeBaseID string) error {
	return r.db.WithContext(ctx).Where("knowledge_base_id = ?", knowledgeBaseID).Delete(&schema.RAGChunk{}).Error
}
