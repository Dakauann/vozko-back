package rag

import (
	"context"

	"vozko/domain/shared"
)

type KnowledgeBaseRepository interface {
	Create(ctx context.Context, kb *KnowledgeBase) error
	Update(ctx context.Context, kb *KnowledgeBase) error
	Delete(ctx context.Context, id string) error
	FindByID(ctx context.Context, id string) (*KnowledgeBase, error)
	FindByWorkspace(ctx context.Context, workspaceID string, opts shared.Pagination) (*shared.PaginatedResult[*KnowledgeBase], error)
	FindByWorkspaceAndDepartment(ctx context.Context, workspaceID string, departmentID *string, opts shared.Pagination) (*shared.PaginatedResult[*KnowledgeBase], error)
	FindByIDs(ctx context.Context, ids []string) ([]*KnowledgeBase, error)
	CountByWorkspace(ctx context.Context, workspaceID string) (int, error)
	IncrementDocumentCount(ctx context.Context, id string, delta int) error
	IncrementChunkCount(ctx context.Context, id string, delta int) error
	UpdateStats(ctx context.Context, id string, docCount int, chunkCount int, sizeMB float64) error
}

type DocumentRepository interface {
	Create(ctx context.Context, doc *Document) error
	Update(ctx context.Context, doc *Document) error
	Delete(ctx context.Context, id string) error
	FindByID(ctx context.Context, id string) (*Document, error)
	FindByIDs(ctx context.Context, ids []string) ([]*Document, error)
	FindByKnowledgeBase(ctx context.Context, knowledgeBaseID string, opts shared.Pagination) (*shared.PaginatedResult[*Document], error)
	FindPending(ctx context.Context, limit int) ([]*Document, error)
	UpdateStatus(ctx context.Context, id string, status DocumentStatus, errMsg string) error
	CountByKnowledgeBase(ctx context.Context, knowledgeBaseID string) (int, error)
}

type ChunkNeighborRequest struct {
	DocumentID string
	MinIndex   int
	MaxIndex   int
}

type ChunkRepository interface {
	CreateBatch(ctx context.Context, chunks []*Chunk) error
	DeleteByDocument(ctx context.Context, documentID string) error
	DeleteByKnowledgeBase(ctx context.Context, knowledgeBaseID string) error
	FindByDocument(ctx context.Context, documentID string) ([]*Chunk, error)
	CountByKnowledgeBase(ctx context.Context, knowledgeBaseID string) (int, error)

	FindByDocumentAndIndexRange(ctx context.Context, documentID string, minIndex int, maxIndex int) ([]*Chunk, error)

	FindNeighborsBatch(ctx context.Context, requests []ChunkNeighborRequest) ([]*Chunk, error)
}

type VectorRepository interface {
	SimilaritySearch(ctx context.Context, input SimilaritySearchInput) (*SimilaritySearchResult, error)
	UpsertChunks(ctx context.Context, chunks []*Chunk) error
	DeleteByDocument(ctx context.Context, documentID string) error
	DeleteByKnowledgeBase(ctx context.Context, knowledgeBaseID string) error
}

type AgentKnowledgeBaseRepository interface {
	Link(ctx context.Context, agentID string, knowledgeBaseID string) error
	Unlink(ctx context.Context, agentID string, knowledgeBaseID string) error
	FindByAgent(ctx context.Context, agentID string) ([]string, error)
	FindByKnowledgeBase(ctx context.Context, knowledgeBaseID string) ([]string, error)
	IsLinked(ctx context.Context, agentID string, knowledgeBaseID string) (bool, error)
	ReplaceForAgent(ctx context.Context, agentID string, knowledgeBaseIDs []string) error
}
