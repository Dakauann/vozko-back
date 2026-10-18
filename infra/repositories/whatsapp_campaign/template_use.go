package whatsapp_campaign_repository

import (
	"time"

	"gorm.io/gorm"

	wc "vozko/domain/whatsapp_campaign"
	"vozko/infra/database/schema"
)

type TemplateUse struct {
	db *gorm.DB
}

func NewTemplateUse(db *gorm.DB) *TemplateUse {
	return &TemplateUse{db: db}
}

func (u *TemplateUse) BusyWithTemplate(templateID string, at time.Time) (bool, error) {
	var count int64
	err := u.db.Model(&schema.WhatsAppCampaign{}).
		Where("template_id = ? AND (status = ? OR (status = ? AND scheduled_start > ?))",
			templateID, string(wc.CampaignStatusRunning), string(wc.CampaignStatusStopped), at.UTC()).
		Count(&count).Error
	return count > 0, err
}
