package schema

import (
	"time"

	"gorm.io/datatypes"
)

type TemplateButtonAction struct {
	WorkspaceID  string         `gorm:"type:uuid;primaryKey"`
	TemplateName string         `gorm:"type:varchar(512);primaryKey"`
	Language     string         `gorm:"type:varchar(10);primaryKey"`
	ButtonIndex  int            `gorm:"primaryKey;autoIncrement:false"`
	Kind         string         `gorm:"type:varchar(32);not null"`
	Params       datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'"`
	Confirmation string         `gorm:"type:text;not null;default:''"`
	ButtonText   string         `gorm:"type:varchar(64);not null"`
	Version      int64          `gorm:"not null"`
	UpdatedBy    string         `gorm:"type:varchar(64);not null;default:''"`
	UpdatedAt    time.Time      `gorm:"type:timestamptz;not null"`
}

func (TemplateButtonAction) TableName() string { return "template_button_actions" }

type TemplateExitSettings struct {
	WorkspaceID  string    `gorm:"type:uuid;primaryKey"`
	TemplateName string    `gorm:"type:varchar(512);primaryKey"`
	Language     string    `gorm:"type:varchar(10);primaryKey"`
	Confirmation string    `gorm:"type:text;not null;default:''"`
	Version      int64     `gorm:"not null"`
	UpdatedBy    string    `gorm:"type:varchar(64);not null;default:''"`
	UpdatedAt    time.Time `gorm:"type:timestamptz;not null"`
}

func (TemplateExitSettings) TableName() string { return "template_exit_settings" }

type WhatsAppCampaignButtonRoutes struct {
	CampaignID             string         `gorm:"type:uuid;primaryKey"`
	WorkspaceID            string         `gorm:"type:uuid;not null;index"`
	TemplateName           string         `gorm:"type:varchar(512);not null"`
	Language               string         `gorm:"type:varchar(10);not null"`
	Routes                 datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"`
	Version                int64          `gorm:"not null"`
	UpdatedBy              string         `gorm:"type:varchar(64);not null;default:''"`
	UpdatedAt              time.Time      `gorm:"type:timestamptz;not null"`
	ExitRiskAcknowledgedBy *string        `gorm:"type:varchar(64)"`
	ExitRiskAcknowledgedAt *time.Time     `gorm:"type:timestamptz"`
}

func (WhatsAppCampaignButtonRoutes) TableName() string { return "whatsapp_campaign_button_routes" }
