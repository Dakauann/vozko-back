package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type SavedView struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index:idx_saved_view_ws_owner_object,priority:1"`
	OwnerID     string `gorm:"type:uuid;not null;index:idx_saved_view_ws_owner_object,priority:2"`
	ObjectType  string `gorm:"type:varchar(20);not null;index:idx_saved_view_ws_owner_object,priority:3"`
	PipelineID  string `gorm:"type:uuid;default:null"`
	Name        string `gorm:"not null;size:120"`

	Filter     datatypes.JSON `gorm:"type:jsonb"`
	GroupBy    string         `gorm:"type:varchar(30);not null;default:stage"`
	GroupByKey string         `gorm:"size:120;default:null"`
	SortField  string         `gorm:"size:60;default:null"`
	SortDir    string         `gorm:"type:varchar(4);default:null"`
	Columns    datatypes.JSON `gorm:"type:jsonb"`

	Visibility string `gorm:"type:varchar(10);not null;default:private"`
	IsDefault  bool   `gorm:"default:false"`
	Position   int    `gorm:"default:0"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (SavedView) TableName() string {
	return "saved_views"
}

func (v *SavedView) BeforeCreate(tx *gorm.DB) error {
	if v.ID == "" {
		v.ID = uuid.New().String()
	}
	return nil
}
