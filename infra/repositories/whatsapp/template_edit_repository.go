package whatsapp_repository

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"vozko/domain/whatsapp/template"
	"vozko/infra/database/schema"
)

type TemplateEditLog struct {
	db *gorm.DB
}

var _ template.EditLog = (*TemplateEditLog)(nil)

func NewTemplateEditLog(db *gorm.DB) *TemplateEditLog {
	return &TemplateEditLog{db: db}
}

func (l *TemplateEditLog) Record(record template.EditRecord) error {
	before, err := json.Marshal(record.Before)
	if err != nil {
		return err
	}
	after, err := json.Marshal(record.After)
	if err != nil {
		return err
	}
	return l.db.Create(&schema.WhatsAppTemplateEdit{
		TemplateID: record.TemplateID, EditedAt: record.At.UTC(), WorkspaceID: record.WorkspaceID, UserID: record.UserID,
		Before: before, After: after,
	}).Error
}

func (l *TemplateEditLog) EditedSince(templateID string, since time.Time) ([]time.Time, error) {
	var at []time.Time
	err := l.db.Model(&schema.WhatsAppTemplateEdit{}).
		Where("template_id = ? AND edited_at >= ?", templateID, since.UTC()).
		Order("edited_at").
		Pluck("edited_at", &at).Error
	return at, err
}
