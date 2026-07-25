package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type WorkspaceTemplateAccess struct {
	ID          string    `gorm:"primaryKey;type:uuid"`
	WorkspaceID string    `gorm:"index;not null;type:uuid;uniqueIndex:idx_wta_workspace_template,priority:1"`
	TemplateID  string    `gorm:"index;not null;type:uuid;uniqueIndex:idx_wta_workspace_template,priority:2"`
	GrantedBy   string    `gorm:"not null;type:uuid"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`

	Workspace Workspace        `gorm:"foreignKey:WorkspaceID;references:ID"`
	Template  WhatsAppTemplate `gorm:"foreignKey:TemplateID;references:ID"`
	Granter   User             `gorm:"foreignKey:GrantedBy;references:ID"`
}

func (WorkspaceTemplateAccess) TableName() string {
	return "workspace_template_access"
}

func (a *WorkspaceTemplateAccess) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	return nil
}
