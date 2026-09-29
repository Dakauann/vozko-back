package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/infra/crypto/piigorm"
)

type SIPTrunk struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index:idx_sip_trunk_ws_del,priority:1"`
	Name        string `gorm:"size:120;not null"`
	TrunkType   string `gorm:"size:16;not null"`

	Host      string                  `gorm:"size:255;not null"`
	Port      int                     `gorm:"not null;default:0"`
	Domain    string                  `gorm:"size:255"`
	Transport string                  `gorm:"size:8;not null"`
	Username  string                  `gorm:"size:128"`
	Password  piigorm.EncryptedString `gorm:"type:bytea" json:"-"`

	Enabled  bool           `gorm:"not null;index:idx_sip_trunk_enabled"`
	Settings datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'"`

	RegistrationStatus string `gorm:"size:16;not null;default:'UNREGISTERED'"`
	LastError          string `gorm:"size:500"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index;index:idx_sip_trunk_ws_del,priority:2"`
}

func (SIPTrunk) TableName() string { return "sip_trunks" }

func (t *SIPTrunk) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	return nil
}
