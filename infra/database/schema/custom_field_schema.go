package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// CustomFieldDefinition is the GORM row model for a workspace-defined custom
// field schema attached to a CRM object (opportunity | conversation | lead). It
// is kept separate from the domain customfield.Definition entity and hand-mapped
// in the repository. The select/multiselect Options list is stored as jsonb via
// datatypes.JSON. The (workspace_id, object_type, key) triple is unique so a key
// is stable per object within a workspace.
//
// Added to AutoMigrate in migrate.go (see WIRING notes).
type CustomFieldDefinition struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;uniqueIndex:idx_custom_field_ws_object_key,priority:1"`
	ObjectType  string `gorm:"type:varchar(20);not null;uniqueIndex:idx_custom_field_ws_object_key,priority:2"`
	Key         string `gorm:"size:120;not null;uniqueIndex:idx_custom_field_ws_object_key,priority:3"`

	Label   string         `gorm:"size:200;not null"`
	Type    string         `gorm:"type:varchar(20);not null"`
	Options datatypes.JSON `gorm:"type:jsonb"`

	Required bool `gorm:"not null;default:false"`
	Position int  `gorm:"not null;default:0"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (CustomFieldDefinition) TableName() string {
	return "custom_field_definitions"
}

func (d *CustomFieldDefinition) BeforeCreate(tx *gorm.DB) error {
	if d.ID == "" {
		d.ID = uuid.New().String()
	}
	return nil
}
