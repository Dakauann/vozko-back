package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Category struct {
	ID          string         `gorm:"primaryKey;type:text"`
	Name        string         `gorm:"not null;size:255"`
	Slug        string         `gorm:"not null;size:255;uniqueIndex"`
	Description string         `gorm:"type:text"`
	ParentID    *string        `gorm:"type:text;index"`
	Parent      *Category      `gorm:"foreignKey:ParentID"`
	Children    []Category     `gorm:"foreignKey:ParentID"`
	CreatedAt   time.Time      `gorm:"autoCreateTime"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime"`
	DeletedAt   gorm.DeletedAt `gorm:"index"`
}

func (Category) TableName() string {
	return "categories"
}

func (c *Category) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}
