package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	CustomFieldLiveKeyIndex  = "ux_custom_field_ws_object_key_live"
	CustomFieldLiveRoleIndex = "ux_custom_field_ws_object_role_live"
)

type CustomFieldDefinition struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null"`
	ObjectType  string `gorm:"type:varchar(20);not null"`
	Key         string `gorm:"size:120;not null"`

	Label       string         `gorm:"size:200;not null"`
	Type        string         `gorm:"type:varchar(20);not null"`
	Options     datatypes.JSON `gorm:"type:jsonb"`
	OptionTones datatypes.JSON `gorm:"type:jsonb"`

	Required   bool   `gorm:"not null;default:false"`
	Sensitive  bool   `gorm:"default:false"`
	LegalBasis string `gorm:"size:500"`
	Role       string `gorm:"type:varchar(32)"`
	Position   int    `gorm:"not null;default:0"`

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
