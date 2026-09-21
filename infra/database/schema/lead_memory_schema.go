package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	leadmemory "vozko/domain/lead_memory"
)

type LeadMemory struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	LeadID      string `gorm:"type:uuid;not null"`

	Category    string `gorm:"size:32;not null"`
	Content     string `gorm:"type:text;not null"`
	ContentNorm string `gorm:"type:text;not null"`

	ActorKind string `gorm:"size:16;not null"`
	ActorID   string `gorm:"type:text;not null"`

	SourceEntryID   *string `gorm:"type:text"`
	SourceEntryType *string `gorm:"size:32"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`

	Workspace *Workspace `gorm:"foreignKey:WorkspaceID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
	Lead      *Lead      `gorm:"foreignKey:LeadID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
}

func (LeadMemory) TableName() string {
	return "lead_memories"
}

func (m *LeadMemory) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	if m.ContentNorm == "" {
		m.ContentNorm = leadmemory.NormalizeContent(m.Content)
	}
	return nil
}
