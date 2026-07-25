package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/infra/crypto/piigorm"
)

type SIPTrunk struct {
	ID                 string  `gorm:"primaryKey;type:uuid"`
	WorkspaceID        string  `gorm:"type:uuid;not null;index"`
	OwnerAssignedBy    *string `gorm:"type:uuid"` // nil → NULL; empty string is not a valid uuid
	OwnerAssignedAt    *time.Time
	Name               string                  `gorm:"size:255;not null;index"`
	Description        string                  `gorm:"size:1000"`
	Host               string                  `gorm:"size:255;not null"`
	Port               int                     `gorm:"not null;default:5060"`
	Username           string                  `gorm:"size:255;not null"`
	Password           piigorm.EncryptedString `gorm:"column:password;type:bytea;not null"`
	Domain             string                  `gorm:"size:255"`
	Transport          string                  `gorm:"size:10;not null;default:udp"`
	TrunkType          string                  `gorm:"size:20;not null"`
	IsRotational       bool                    `gorm:"not null;default:false"`
	PhoneNumber        string                  `gorm:"size:20"`
	Enabled            bool                    `gorm:"not null;default:true;index"`
	IsGloballyVisible  bool                    `gorm:"not null;default:false;index"`
	RegistrationStatus string                  `gorm:"size:20;not null;default:pending"`
	LastError          string                  `gorm:"size:1000"`
	LastRegisteredAt   *time.Time              `gorm:"index"`
	CreatedAt          time.Time               `gorm:"autoCreateTime"`
	UpdatedAt          time.Time               `gorm:"autoUpdateTime"`

	OutboundProxy *string `gorm:"size:255"`
	AuthUsername  *string `gorm:"size:128"`
	UserAgent     *string `gorm:"size:128"`

	RegisterEnabled       *bool
	RegisterExpirySeconds *int
	RegisterRetrySeconds  *int
	SessionTimerEnabled   *bool
	SessionTimerSeconds   *int
	MinSESeconds          *int
	SessionRefresher      *string `gorm:"size:8"`

	BindHost      *string `gorm:"size:64"`
	BindPort      *int
	PublicAddress *string        `gorm:"size:255"`
	StunServers   datatypes.JSON `gorm:"type:jsonb"`
	StunEnabled   *bool

	Codecs          datatypes.JSON `gorm:"type:jsonb"`
	PTimeMs         *int
	DTMFPayloadType *int
	SRTPMode        *string `gorm:"size:16"`

	DialPrefix      *string `gorm:"size:16"`
	DialStripDigits *int
	NumberFormat    *string `gorm:"size:16"`
	HideCallerID    *bool

	RingingTimeoutSeconds *int
}

func (SIPTrunk) TableName() string {
	return "sip_trunks"
}

func (t *SIPTrunk) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	return nil
}
