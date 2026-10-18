package template_usecase

import (
	"fmt"
	"time"

	"vozko/domain/whatsapp/quality"
	domain "vozko/domain/whatsapp/template"
)

const recentHealthChanges = 20

type GrantedTemplate interface {
	Get(workspaceID, templateID string) (*domain.Template, error)
}

type TemplateHealth struct {
	TemplateID string
	Status     domain.TemplateStatus
	Category   domain.TemplateCategory
	Quality    string
	QualityAt  time.Time
	Flagged    bool
	Changes    []quality.Change
}

type TemplateHealthReader struct {
	templates GrantedTemplate
	record    quality.Reader
}

func NewTemplateHealthReader(templates GrantedTemplate, record quality.Reader) *TemplateHealthReader {
	return &TemplateHealthReader{templates: templates, record: record}
}

func (r *TemplateHealthReader) Read(workspaceID, templateID string) (TemplateHealth, error) {
	tmpl, err := r.templates.Get(workspaceID, templateID)
	if err != nil {
		return TemplateHealth{}, err
	}
	latest, err := r.record.LatestBySignal(quality.SubjectTemplate, tmpl.ExternalID)
	if err != nil {
		return TemplateHealth{}, fmt.Errorf("read template %s health: %w", templateID, err)
	}
	changes, err := r.record.Recent(quality.SubjectTemplate, tmpl.ExternalID, recentHealthChanges)
	if err != nil {
		return TemplateHealth{}, fmt.Errorf("read template %s changes: %w", templateID, err)
	}
	reading := quality.ReadTemplate(latest)
	return TemplateHealth{
		TemplateID: tmpl.ID,
		Status:     tmpl.Status,
		Category:   tmpl.Category,
		Quality:    reading.Quality,
		QualityAt:  reading.QualityAt,
		Flagged:    reading.Flagged,
		Changes:    changes,
	}, nil
}
