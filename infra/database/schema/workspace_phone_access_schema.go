package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type WorkspacePhoneAccess struct {
	ID          string    `gorm:"primaryKey;type:uuid"`
	WorkspaceID string    `gorm:"index;not null;type:uuid;uniqueIndex:idx_wpa_workspace_phone,priority:1"`
	PhoneID     string    `gorm:"index;not null;type:uuid;uniqueIndex:idx_wpa_workspace_phone,priority:2"`
	GrantedBy   string    `gorm:"not null;type:uuid"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`

	Workspace Workspace                   `gorm:"foreignKey:WorkspaceID;references:ID"`
	Phone     WhatsAppBusinessPhoneNumber `gorm:"foreignKey:PhoneID;references:ID"`
	Granter   User                        `gorm:"foreignKey:GrantedBy;references:ID"`
}

func (WorkspacePhoneAccess) TableName() string {
	return "workspace_phone_access"
}

func (a *WorkspacePhoneAccess) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	return nil
}
