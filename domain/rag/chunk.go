package rag

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrChunkNotFound          = errors.New("chunk not found")
	ErrChunkIDRequired        = errors.New("chunk id is required")
	ErrChunkDocumentRequired  = errors.New("document id is required")
	ErrChunkContentRequired   = errors.New("chunk content is required")
	ErrChunkEmbeddingRequired = errors.New("chunk embedding is required")
)

type Chunk struct {
	ID              string            `json:"id"`
	DocumentID      string            `json:"documentId"`
	KnowledgeBaseID string            `json:"knowledgeBaseId"`
	Content         string            `json:"content"`
	Embedding       []float32         `json:"embedding"`
	Index           int               `json:"index"`
	StartOffset     int               `json:"startOffset"`
	EndOffset       int               `json:"endOffset"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	TokenCount      int               `json:"tokenCount"`
	CreatedAt       time.Time         `json:"createdAt"`
}

func (c *Chunk) Normalize() {
	c.Content = strings.TrimSpace(c.Content)
	if c.Metadata == nil {
		c.Metadata = make(map[string]string)
	}
}

func (c *Chunk) Validate() error {
	if c.ID == "" {
		return ErrChunkIDRequired
	}
	if c.DocumentID == "" {
		return ErrChunkDocumentRequired
	}
	if c.Content == "" {
		return ErrChunkContentRequired
	}
	return nil
}

type ChunkWithScore struct {
	Chunk      *Chunk  `json:"chunk"`
	Score      float32 `json:"score"`
	DocumentID string  `json:"documentId"`
}

type SimilaritySearchInput struct {
	KnowledgeBaseIDs []string
	QueryEmbedding   []float32
	TopK             int
	MinScore         float32
}

type SimilaritySearchResult struct {
	Chunks     []ChunkWithScore `json:"chunks"`
	TotalFound int              `json:"totalFound"`
}
