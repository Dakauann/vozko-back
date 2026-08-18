package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type WhatsAppCampaign struct {
	ID              string  `gorm:"primaryKey;type:uuid"`
	WorkspaceID     string  `gorm:"type:uuid;not null;index;index:idx_wa_campaign_ws_del,priority:1"`
	DepartmentID    *string `gorm:"type:uuid;index"`
	BusinessPhoneID *string `gorm:"type:uuid;index"`
	Name            string  `gorm:"size:255;not null"`
	Type            string  `gorm:"size:20;not null;default:'standard'"`
	TemplateID      *string `gorm:"type:uuid;index"`
	AgentID         *string `gorm:"type:uuid;index"`
	WorkflowID      *string `gorm:"type:uuid;index"`
	// PipelineID is the campaign's conversation funnel (null = workspace default).
	// The board, AI stage classifier and initial-stage all resolve through it.
	PipelineID           *string        `gorm:"type:uuid;index"`
	EnableAgentResponses bool           `gorm:"not null;default:false"`
	EnableWorkflow       bool           `gorm:"not null;default:false"`
	EnableAnalysis       bool           `gorm:"not null;default:false"`
	EnableAutoStaging    bool           `gorm:"not null;default:false"`
	EnableAutoMemory     bool           `gorm:"not null;default:false"`
	PreferAudio          bool           `gorm:"not null;default:false"`
	ShowTemplateInCrm    bool           `gorm:"column:show_template_in_crm;not null;default:false"`
	AiModel              string         `gorm:"size:120"`
	Status               string         `gorm:"size:20;not null;default:STOPPED"`
	ResetCode            string         `gorm:"size:10"`
	ClearCode            string         `gorm:"size:10"`
	Archived             bool           `gorm:"default:false"`
	CreatedAt            time.Time      `gorm:"autoCreateTime"`
	UpdatedAt            time.Time      `gorm:"autoUpdateTime"`
	DeletedAt            gorm.DeletedAt `gorm:"index;index:idx_wa_campaign_ws_del,priority:2"`
	ScheduledStart       time.Time      `gorm:"type:timestamptz;index"`

	Workspace     Workspace                   `gorm:"foreignKey:WorkspaceID;references:ID"`
	BusinessPhone WhatsAppBusinessPhoneNumber `gorm:"foreignKey:BusinessPhoneID;references:ID"`
}

func (WhatsAppCampaign) TableName() string {
	return "whatsapp_campaigns"
}

func (c *WhatsAppCampaign) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}
