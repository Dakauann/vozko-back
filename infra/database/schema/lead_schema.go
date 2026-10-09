package schema

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type OptionalText string

func (t OptionalText) Value() (driver.Value, error) {
	if t == "" {
		return nil, nil
	}
	return string(t), nil
}

func (t *OptionalText) Scan(value interface{}) error {
	switch v := value.(type) {
	case nil:
		*t = ""
	case string:
		*t = OptionalText(v)
	case []byte:
		*t = OptionalText(v)
	default:
		return errors.New("failed to scan OptionalText: unsupported type")
	}
	return nil
}

type Lead struct {
	ID                   string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID          string         `gorm:"type:uuid;not null;index:idx_leads_workspace_id;uniqueIndex:ux_leads_workspace_number,where:deleted_at IS NULL"`
	Workspace            *Workspace     `gorm:"foreignKey:WorkspaceID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
	Number               OptionalText   `gorm:"size:20;uniqueIndex:ux_leads_workspace_number,where:deleted_at IS NULL"`
	Name                 string         `gorm:"size:255"`
	NameSource           OptionalText   `gorm:"type:varchar(16)"`
	Nickname             OptionalText   `gorm:"type:varchar(120)"`
	Email                OptionalText   `gorm:"type:varchar(254)"`
	BirthDate            CalendarDate   `gorm:"type:date"`
	Source               OptionalText   `gorm:"type:varchar(16)"`
	OwnerID              OptionalText   `gorm:"type:uuid"`
	OwnerKind            OptionalText   `gorm:"type:varchar(16)"`
	CustomFields         datatypes.JSON `gorm:"type:jsonb"`
	WhatsAppOptInAt      *time.Time     `gorm:"column:whatsapp_opt_in_at;type:timestamptz"`
	WhatsAppOptInSource  OptionalText   `gorm:"column:whatsapp_opt_in_source;type:varchar(32)"`
	WhatsAppOptInPurpose OptionalText   `gorm:"column:whatsapp_opt_in_purpose;type:varchar(200)"`
	OptedOutAt           *time.Time     `gorm:"type:timestamptz"`
	OptedOutSource       OptionalText   `gorm:"column:opted_out_source;type:varchar(32)"`
	ProfilePictureURL    string         `gorm:"size:1024"`
	Age                  *int           `gorm:"type:int"`
	Blocked              bool           `gorm:"not null;default:false;index:idx_leads_blocked"`
	BlockedAt            *time.Time     `gorm:"type:timestamptz"`
	BlockedBy            *string        `gorm:"type:uuid"`
	RelativesCount       int            `gorm:"not null;default:0"`
	ReferredCount        int            `gorm:"not null;default:0"`
	Version              int64          `gorm:"not null;default:1"`
	CreatedAt            time.Time      `gorm:"autoCreateTime"`
	UpdatedAt            time.Time      `gorm:"autoUpdateTime"`
	DeletedAt            gorm.DeletedAt `gorm:"index"`
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

type LeadEvent struct {
	ID          string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID string         `gorm:"type:uuid;not null;index:idx_lead_events_workspace"`
	LeadID      string         `gorm:"type:uuid;not null;index:idx_lead_events_lead_created,priority:1"`
	Lead        *Lead          `gorm:"foreignKey:LeadID;references:ID;constraint:OnDelete:CASCADE"`
	ActorID     OptionalText   `gorm:"type:uuid"`
	ActorKind   string         `gorm:"type:varchar(16);not null"`
	Kind        string         `gorm:"type:varchar(32);not null"`
	Changes     datatypes.JSON `gorm:"type:jsonb;not null"`
	CreatedAt   time.Time      `gorm:"not null;index:idx_lead_events_lead_created,priority:2,sort:desc"`
}

func (LeadEvent) TableName() string {
	return "lead_events"
}
