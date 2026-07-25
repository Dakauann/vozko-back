package schema

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Lead struct {
	ID                string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID       string         `gorm:"type:uuid;not null;index:idx_leads_workspace_id;uniqueIndex:ux_leads_workspace_number,where:deleted_at IS NULL"`
	Workspace         *Workspace     `gorm:"foreignKey:WorkspaceID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
	Number            string         `gorm:"size:20;not null;uniqueIndex:ux_leads_workspace_number,where:deleted_at IS NULL"`
	Name              string         `gorm:"size:255"`
	ProfilePictureURL string         `gorm:"size:1024"`
	Age               *int           `gorm:"type:int"`
	Blocked           bool           `gorm:"not null;default:false;index:idx_leads_blocked"`
	BlockedAt         *time.Time     `gorm:"type:timestamptz"`
	BlockedBy         *string        `gorm:"type:uuid"`
	CreatedAt         time.Time      `gorm:"autoCreateTime"`
	UpdatedAt         time.Time      `gorm:"autoUpdateTime"`
	DeletedAt         gorm.DeletedAt `gorm:"index"`
}

func (Lead) TableName() string {
	return "leads"
}

func (l *Lead) BeforeCreate(tx *gorm.DB) error {
	if l.ID == "" {
		l.ID = uuid.New().String()
	}
	return nil
}

type LeadMetadata map[string]interface{}

func (m *LeadMetadata) Scan(value interface{}) error {
	if value == nil {
		*m = make(LeadMetadata)
		return nil
	}

	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return errors.New("failed to unmarshal LeadMetadata: unsupported type")
	}

	if len(bytes) == 0 {
		*m = make(LeadMetadata)
		return nil
	}
	return json.Unmarshal(bytes, m)
}

func (m LeadMetadata) Value() (driver.Value, error) {
	if m == nil {
		return "{}", nil
	}
	return json.Marshal(m)
}
