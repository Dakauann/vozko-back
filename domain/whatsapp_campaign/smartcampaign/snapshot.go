package smartcampaign

import (
	"time"

	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/template"
)

type Signal string

const (
	SignalTemplate Signal = "template"
	SignalPhone    Signal = "phone"
)

type TemplateState struct {
	Status     template.TemplateStatus
	StatusAt   time.Time
	Flagged    bool
	Quality    businessphone.QualityRating
	QualityAt  time.Time
	Category   template.TemplateCategory
	CategoryAt time.Time
	CreatedAs  template.TemplateCategory
}

func (t TemplateState) Reclassified() bool {
	return t.CreatedAs != "" && t.CreatedAs != template.TemplateCategoryMarketing && t.Category == template.TemplateCategoryMarketing
}

type PhoneState struct {
	Status    businessphone.Status
	Quality   businessphone.QualityRating
	ChangedAt time.Time
}

type Snapshot struct {
	Now        time.Time
	Template   TemplateState
	Phone      PhoneState
	Unread     []Signal
	Wave       Outcome
	Pending    int
	CapRoom    int
	WavesToday int
}
