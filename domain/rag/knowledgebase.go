package rag

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrKnowledgeBaseNotFound     = errors.New("knowledge base not found")
	ErrKnowledgeBaseIDRequired   = errors.New("knowledge base id is required")
	ErrKnowledgeBaseNameRequired = errors.New("knowledge base name is required")
	ErrKnowledgeBaseNameTooLong  = errors.New("knowledge base name must not exceed 120 characters")
	ErrWorkspaceIDRequired       = errors.New("workspace id is required")
	ErrMaxKnowledgeBasesReached  = errors.New("maximum number of knowledge bases reached (5)")
	ErrMaxDocumentsReached       = errors.New("maximum number of documents per knowledge base reached (120)")
	ErrMaxTotalSizeReached       = errors.New("knowledge base total size limit reached (512 MB)")
)

const (
	MaxKnowledgeBasesPerWorkspace = 5
	MaxDocumentsPerKnowledgeBase  = 120
	MaxTotalSizeMB                = 512
)

type KnowledgeBaseStatus string

const (
	KnowledgeBaseStatusActive   KnowledgeBaseStatus = "active"
	KnowledgeBaseStatusInactive KnowledgeBaseStatus = "inactive"
)

type ChunkingStrategy string

const (
	ChunkingStrategyFixedSize ChunkingStrategy = "fixed_size"
	ChunkingStrategySentence  ChunkingStrategy = "sentence"
	ChunkingStrategyParagraph ChunkingStrategy = "paragraph"
	ChunkingStrategySemantic  ChunkingStrategy = "semantic"
)

func (s ChunkingStrategy) IsValid() bool {
	switch s {
	case ChunkingStrategyFixedSize, ChunkingStrategySentence, ChunkingStrategyParagraph, ChunkingStrategySemantic:
		return true
	default:
		return false
	}
}

const (
	DefaultEmbeddingModel = "bge-m3"
	DefaultEmbeddingDim   = 1024
)

type KnowledgeBaseConfig struct {
	ChunkingStrategy ChunkingStrategy `json:"chunkingStrategy"`
	ChunkSize        int              `json:"chunkSize"`
	ChunkOverlap     int              `json:"chunkOverlap"`
	EmbeddingModel   string           `json:"embeddingModel"`
	EmbeddingDim     int              `json:"embeddingDim"`
}

func DefaultKnowledgeBaseConfig() KnowledgeBaseConfig {
	return KnowledgeBaseConfig{
		ChunkingStrategy: ChunkingStrategySentence,
		ChunkSize:        800,
		ChunkOverlap:     100,
		EmbeddingModel:   DefaultEmbeddingModel,
		EmbeddingDim:     DefaultEmbeddingDim,
	}
}

type KnowledgeBase struct {
	ID            string              `json:"id"`
	WorkspaceID   string              `json:"workspaceId"`
	DepartmentID  string              `json:"departmentId,omitempty"`
	Name          string              `json:"name"`
	Description   string              `json:"description"`
	Status        KnowledgeBaseStatus `json:"status"`
	Config        KnowledgeBaseConfig `json:"config"`
	DocumentCount int                 `json:"documentCount"`
	ChunkCount    int                 `json:"chunkCount"`
	TotalSizeMB   float64             `json:"totalSizeMB"`
	CreatedAt     time.Time           `json:"createdAt"`
	UpdatedAt     time.Time           `json:"updatedAt"`
}

func (kb *KnowledgeBase) Normalize() {
	kb.Name = strings.TrimSpace(kb.Name)
	kb.Description = strings.TrimSpace(kb.Description)
	kb.WorkspaceID = strings.TrimSpace(kb.WorkspaceID)

	if kb.Status == "" {
		kb.Status = KnowledgeBaseStatusActive
	}

	if !kb.Config.ChunkingStrategy.IsValid() {
		kb.Config.ChunkingStrategy = ChunkingStrategyFixedSize
	}
	if kb.Config.ChunkSize <= 0 {
		kb.Config.ChunkSize = 512
	}
	if kb.Config.ChunkOverlap < 0 {
		kb.Config.ChunkOverlap = 50
	}
	if kb.Config.EmbeddingModel == "" {
		kb.Config.EmbeddingModel = DefaultEmbeddingModel
	}
	if kb.Config.EmbeddingDim <= 0 {
		kb.Config.EmbeddingDim = DefaultEmbeddingDim
	}
}

func (kb *KnowledgeBase) Validate() error {
	if kb.ID == "" {
		return ErrKnowledgeBaseIDRequired
	}
	if kb.WorkspaceID == "" {
		return ErrWorkspaceIDRequired
	}
	if kb.Name == "" {
		return ErrKnowledgeBaseNameRequired
	}
	if len(kb.Name) > 120 {
		return ErrKnowledgeBaseNameTooLong
	}
	return nil
}

func (kb *KnowledgeBase) IsActive() bool {
	return kb.Status == KnowledgeBaseStatusActive
}

func (kb *KnowledgeBase) Deactivate() {
	kb.Status = KnowledgeBaseStatusInactive
	kb.UpdatedAt = time.Now()
}

func (kb *KnowledgeBase) Activate() {
	kb.Status = KnowledgeBaseStatusActive
	kb.UpdatedAt = time.Now()
}
