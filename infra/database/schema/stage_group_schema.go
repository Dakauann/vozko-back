package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type StageGroup struct {
	ID           string           `gorm:"primaryKey;type:text"`
	WorkspaceID  string           `gorm:"type:uuid;not null;index:idx_tag_group_workspace"`
	DepartmentID *string          `gorm:"type:uuid;index:idx_tag_group_department"`
	Name         string           `gorm:"not null;size:100"`
	CreatedAt    time.Time        `gorm:"autoCreateTime"`
	UpdatedAt    time.Time        `gorm:"autoUpdateTime"`
	DeletedAt    gorm.DeletedAt   `gorm:"index"`
	Items        []StageGroupItem `gorm:"foreignKey:StageGroupID"`
}

func (StageGroup) TableName() string {
	return "stage_groups"
}

func (tg *StageGroup) BeforeCreate(tx *gorm.DB) error {
	if tg.ID == "" {
		tg.ID = uuid.New().String()
	}
	return nil
}

type StageGroupItem struct {
	ID           string         `gorm:"primaryKey;type:text"`
	StageGroupID string         `gorm:"type:uuid;not null;index:idx_tgi_group"`
	Name         string         `gorm:"not null;size:100"`
	Description  string         `gorm:"size:500;default:null"`
	Color        string         `gorm:"size:20"`
	Position     int            `gorm:"default:0"`
	CreatedAt    time.Time      `gorm:"autoCreateTime"`
	UpdatedAt    time.Time      `gorm:"autoUpdateTime"`
	DeletedAt    gorm.DeletedAt `gorm:"index"`
}

func (StageGroupItem) TableName() string {
	return "stage_group_items"
}

func (tgi *StageGroupItem) BeforeCreate(tx *gorm.DB) error {
	if tgi.ID == "" {
		tgi.ID = uuid.New().String()
	}
	return nil
}

type LabelGroup struct {
	ID          string           `gorm:"primaryKey;type:text"`
	WorkspaceID string           `gorm:"type:uuid;not null;index:idx_label_group_workspace"`
	Name        string           `gorm:"not null;size:100"`
	CreatedAt   time.Time        `gorm:"autoCreateTime"`
	UpdatedAt   time.Time        `gorm:"autoUpdateTime"`
	DeletedAt   gorm.DeletedAt   `gorm:"index"`
	Items       []LabelGroupItem `gorm:"foreignKey:LabelGroupID"`
}

func (LabelGroup) TableName() string {
	return "label_groups"
}

func (lg *LabelGroup) BeforeCreate(tx *gorm.DB) error {
	if lg.ID == "" {
		lg.ID = uuid.New().String()
	}
	return nil
}

type LabelGroupItem struct {
	ID           string         `gorm:"primaryKey;type:text"`
	LabelGroupID string         `gorm:"type:uuid;not null;index:idx_lgi_group"`
	Name         string         `gorm:"not null;size:100"`
	Color        string         `gorm:"size:20"`
	Position     int            `gorm:"default:0"`
	CreatedAt    time.Time      `gorm:"autoCreateTime"`
	UpdatedAt    time.Time      `gorm:"autoUpdateTime"`
	DeletedAt    gorm.DeletedAt `gorm:"index"`
}

func (LabelGroupItem) TableName() string {
	return "label_group_items"
}

func (lgi *LabelGroupItem) BeforeCreate(tx *gorm.DB) error {
	if lgi.ID == "" {
		lgi.ID = uuid.New().String()
	}
	return nil
}
