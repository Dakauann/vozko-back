package schema

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type WhatsAppTemplate struct {
	ID              string                     `gorm:"primaryKey;type:uuid"`
	ExternalID      string                     `gorm:"size:100;not null;uniqueIndex:idx_whatsapp_templates_external_waba,priority:1"`
	WABAId          string                     `gorm:"column:waba_id;size:36;not null;default:'';index;uniqueIndex:idx_whatsapp_templates_external_waba,priority:2"`
	Name            string                     `gorm:"size:255;not null;index"`
	Language        string                     `gorm:"size:10;not null;default:pt_BR"`
	Category        string                     `gorm:"size:20;not null;default:MARKETING"`
	Status          string                     `gorm:"size:20;not null;default:PENDING;index"`
	ParameterFormat string                     `gorm:"size:20;not null;default:positional"`
	Components      WhatsAppTemplateComponents `gorm:"type:jsonb"`
	HeaderMediaURL  *string                    `gorm:"size:1024"`
	HeaderMediaID   *string                    `gorm:"size:255"`
	CreatedAt       time.Time                  `gorm:"autoCreateTime"`
	UpdatedAt       time.Time                  `gorm:"autoUpdateTime"`
	DeletedAt       gorm.DeletedAt             `gorm:"index"`
}

func (WhatsAppTemplate) TableName() string {
	return "whatsapp_templates"
}

func (t *WhatsAppTemplate) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	return nil
}

type WhatsAppTemplateComponent struct {
	Type       string                   `json:"type"`
	Format     string                   `json:"format,omitempty"`
	Text       string                   `json:"text,omitempty"`
	Parameters []string                 `json:"parameters,omitempty"`
	Buttons    []WhatsAppTemplateButton `json:"buttons,omitempty"`
	Example    *WhatsAppTemplateExample `json:"example,omitempty"`
}

type WhatsAppTemplateButton struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	URL         string `json:"url,omitempty"`
	PhoneNumber string `json:"phone_number,omitempty"`
	Example     string `json:"example,omitempty"`
}

type WhatsAppTemplateExample struct {
	HeaderText      []string                     `json:"header_text,omitempty"`
	HeaderHandle    []string                     `json:"header_handle,omitempty"`
	BodyText        [][]string                   `json:"body_text,omitempty"`
	BodyTextNamed   []WhatsAppTemplateNamedParam `json:"body_text_named_params,omitempty"`
	HeaderTextNamed []WhatsAppTemplateNamedParam `json:"header_text_named_params,omitempty"`
}

type WhatsAppTemplateNamedParam struct {
	ParamName string `json:"param_name"`
	Example   string `json:"example"`
}

type WhatsAppTemplateComponents []WhatsAppTemplateComponent

func (c *WhatsAppTemplateComponents) Scan(value interface{}) error {
	if value == nil {
		*c = []WhatsAppTemplateComponent{}
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("failed to unmarshal WhatsAppTemplateComponents value")
	}

	if len(bytes) == 0 {
		*c = []WhatsAppTemplateComponent{}
		return nil
	}

	return json.Unmarshal(bytes, c)
}

func (c WhatsAppTemplateComponents) Value() (driver.Value, error) {
	if c == nil {
		return "[]", nil
	}
	return json.Marshal(c)
}
