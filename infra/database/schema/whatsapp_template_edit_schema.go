package schema

import (
	"time"

	"gorm.io/datatypes"
)

type WhatsAppTemplateEdit struct {
	ID          int64          `gorm:"primaryKey;autoIncrement"`
	TemplateID  string         `gorm:"type:uuid;not null;index:idx_wa_template_edit_at,priority:1"`
	EditedAt    time.Time      `gorm:"type:timestamptz;not null;index:idx_wa_template_edit_at,priority:2"`
	WorkspaceID string         `gorm:"type:uuid;not null"`
	UserID      string         `gorm:"type:varchar(64);not null"`
	Before      datatypes.JSON `gorm:"type:jsonb;not null"`
	After       datatypes.JSON `gorm:"type:jsonb;not null"`
}

func (WhatsAppTemplateEdit) TableName() string { return "whatsapp_template_edits" }
