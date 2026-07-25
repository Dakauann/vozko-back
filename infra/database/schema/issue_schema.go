package schema

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Issue struct {
	ID          string  `gorm:"primaryKey;size:36"`
	WorkspaceID string  `gorm:"not null;size:36;index"`
	Title       string  `gorm:"not null;size:255"`
	Description string  `gorm:"size:1000"`
	ImageURLs   *string `gorm:"type:text"`
	Status      string  `gorm:"not null;size:20;default:'OPEN';index"`
	CreatedAt   int64   `gorm:"autoCreateTime"`
	UpdatedAt   int64   `gorm:"autoUpdateTime"`
	ClosedAt    *int64  `gorm:"index"`
}

func (Issue) TableName() string {
	return "issues"
}

func (i *Issue) BeforeCreate(tx *gorm.DB) error {
	if i.ID == "" {
		i.ID = uuid.New().String()
	}
	return nil
}
