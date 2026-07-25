package schema

import (
	"time"

	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
)

type KnowledgeBase struct {
	ID            string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID   string         `gorm:"type:uuid;not null;index"`
	DepartmentID  *string        `gorm:"type:uuid;index"`
	Name          string         `gorm:"not null;size:120"`
	Description   string         `gorm:"type:text"`
	Status        string         `gorm:"not null;size:32;default:'active'"`
	Config        string         `gorm:"type:jsonb;default:'{}'"`
	DocumentCount int            `gorm:"default:0"`
	ChunkCount    int            `gorm:"default:0"`
	TotalSizeMB   float64        `gorm:"default:0"`
	CreatedAt     time.Time      `gorm:"autoCreateTime"`
	UpdatedAt     time.Time      `gorm:"autoUpdateTime"`
	DeletedAt     gorm.DeletedAt `gorm:"index"`
}

func (KnowledgeBase) TableName() string {
	return "knowledge_bases"
}

func (kb *KnowledgeBase) BeforeCreate(tx *gorm.DB) error {
	if kb.ID == "" {
		kb.ID = uuid.New().String()
	}
	return nil
}

type RAGDocument struct {
	ID              string         `gorm:"primaryKey;type:uuid"`
	KnowledgeBaseID string         `gorm:"type:uuid;not null;index"`
	Name            string         `gorm:"not null;size:255"`
	Type            string         `gorm:"not null;size:32;default:'text'"`
	Status          string         `gorm:"not null;size:32;default:'pending'"`
	Content         string         `gorm:"type:text"`
	MediaID         *string        `gorm:"type:uuid;index"`
	SizeBytes       int64          `gorm:"default:0"`
	ChunkCount      int            `gorm:"default:0"`
	Metadata        string         `gorm:"type:jsonb;default:'{}'"`
	ErrorMessage    string         `gorm:"type:text"`
	CreatedAt       time.Time      `gorm:"autoCreateTime"`
	UpdatedAt       time.Time      `gorm:"autoUpdateTime"`
	DeletedAt       gorm.DeletedAt `gorm:"index"`
}

func (RAGDocument) TableName() string {
	return "rag_documents"
}

func (d *RAGDocument) BeforeCreate(tx *gorm.DB) error {
	if d.ID == "" {
		d.ID = uuid.New().String()
	}
	return nil
}

type RAGChunk struct {
	ID              string          `gorm:"primaryKey;type:uuid"`
	DocumentID      string          `gorm:"type:uuid;not null;index"`
	KnowledgeBaseID string          `gorm:"type:uuid;not null;index:idx_rag_chunks_kb"`
	Content         string          `gorm:"type:text;not null"`
	Embedding       pgvector.Vector `gorm:"type:vector(1024)"`
	Index           int             `gorm:"not null;default:0"`
	StartOffset     int             `gorm:"default:0"`
	EndOffset       int             `gorm:"default:0"`
	Metadata        string          `gorm:"type:jsonb;default:'{}'"`
	TokenCount      int             `gorm:"default:0"`
	CreatedAt       time.Time       `gorm:"autoCreateTime"`
}

func (RAGChunk) TableName() string {
	return "rag_chunks"
}

func (c *RAGChunk) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}

type AgentKnowledgeBase struct {
	AgentID         string    `gorm:"primaryKey;type:uuid"`
	KnowledgeBaseID string    `gorm:"primaryKey;type:uuid;index"`
	CreatedAt       time.Time `gorm:"autoCreateTime"`
}

func (AgentKnowledgeBase) TableName() string {
	return "agent_knowledge_bases"
}
