package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/infra/crypto/piigorm"
)

// Branch is the GORM row model for a member SIP extension (branch). It is the DDL
// source of truth (added to AutoMigrate in migrate.go). SecretHA1 is stored
// encrypted at rest as bytea via piigorm, mirroring SIPTrunk.Password. Uniqueness
// of (workspace_id, sip_user) is enforced by a raw CREATE UNIQUE INDEX in
// migrate.go. See docs/BRANCH_RAMAL_TRANSFER_PLAN.md.
type Branch struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	MemberID    string `gorm:"type:uuid;not null;index"`
	UserID      string `gorm:"type:uuid;not null;index"`

	SIPUser     string `gorm:"column:sip_user;size:64;not null"`
	DisplayName string `gorm:"size:120"`

	Realm     string                  `gorm:"size:128"`
	SecretHA1 piigorm.EncryptedString `gorm:"column:secret_ha1;type:bytea;not null"`

	Codecs      datatypes.JSON `gorm:"type:jsonb"`
	MaxContacts int            `gorm:"not null;default:2"`
	DND         bool           `gorm:"not null;default:false"`
	// NOTE: the legacy forward_policy jsonb column is intentionally left orphaned
	// in the DB (call forwarding was removed); AutoMigrate never drops columns, so
	// this is non-destructive and the column is simply no longer read or written.

	Enabled            bool       `gorm:"not null;default:true;index"`
	RegistrationStatus string     `gorm:"size:24;not null;default:never_registered"`
	LastRegisteredAt   *time.Time `gorm:"index"`

	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

func (Branch) TableName() string {
	return "branches"
}

func (b *Branch) BeforeCreate(tx *gorm.DB) error {
	if b.ID == "" {
		b.ID = uuid.New().String()
	}
	return nil
}
