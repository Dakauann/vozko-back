package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Agent struct {
	ID                 string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID        string         `gorm:"type:uuid;not null;index"`
	DepartmentID       *string        `gorm:"type:uuid;index"`
	Name               string         `gorm:"not null;size:120"`
	Description        string         `gorm:"type:text"`
	InitialMessage     string         `gorm:"not null;type:text"`
	UseInitialMessage  bool           `gorm:"column:use_initial_message;not null;default:true"`
	MessagingPrompt    string         `gorm:"not null;type:text"`
	MessagingModel     string         `gorm:"size:120;default:'gpt-4o-mini'"`
	AvatarURL          string         `gorm:"size:255"`
	Tags               string         `gorm:"type:jsonb"`
	Metadata           string         `gorm:"type:jsonb"`
	Provider           string         `gorm:"not null;size:32;default:'platform'"`
	ExternalID         string         `gorm:"size:160"`
	WhatsAppTemplateID *string        `gorm:"column:whatsapp_template_id;type:uuid;index"`
	BusinessPhoneID    *string        `gorm:"type:uuid;index"`
	RAGEnabled         bool           `gorm:"default:false"`
	RAGConfig          *string        `gorm:"type:jsonb"`
	IsActive           bool           `gorm:"default:true"`
	Archived           bool           `gorm:"default:false"`
	CreatedAt          time.Time      `gorm:"autoCreateTime"`
	UpdatedAt          time.Time      `gorm:"autoUpdateTime"`
	DeletedAt          gorm.DeletedAt `gorm:"index"`
	ToolBindings       string         `gorm:"type:jsonb"`
	MediaIDs           string         `gorm:"type:jsonb"`
	Variables          string         `gorm:"type:jsonb"`
}

func (Agent) TableName() string {
	return "agents"
}

func (a *Agent) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	return nil
}
