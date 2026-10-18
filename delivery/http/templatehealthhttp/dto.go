package templatehealthhttp

import (
	"time"

	template_usecase "vozko/usecases/whatsapp/template"
)

type Change struct {
	Signal     string    `json:"signal" example:"quality" enums:"quality,status,category"`
	Event      string    `json:"event,omitempty" example:"FLAGGED"`
	Previous   string    `json:"previous,omitempty" example:"GREEN"`
	Current    string    `json:"current" example:"YELLOW"`
	OccurredAt time.Time `json:"occurredAt"`
}

type Response struct {
	TemplateID string     `json:"templateId"`
	Status     string     `json:"status" example:"APPROVED"`
	Category   string     `json:"category" example:"MARKETING"`
	Quality    string     `json:"quality" example:"UNKNOWN" enums:"GREEN,YELLOW,RED,UNKNOWN"`
	QualityAt  *time.Time `json:"qualityAt,omitempty"`
	Flagged    bool       `json:"flagged"`
	Changes    []Change   `json:"changes"`
}

func toResponse(h template_usecase.TemplateHealth) Response {
	changes := make([]Change, len(h.Changes))
	for i, c := range h.Changes {
		changes[i] = Change{Signal: string(c.Signal), Event: c.Event, Previous: c.Previous, Current: c.Current, OccurredAt: c.OccurredAt}
	}
	out := Response{
		TemplateID: h.TemplateID,
		Status:     string(h.Status),
		Category:   string(h.Category),
		Quality:    h.Quality,
		Flagged:    h.Flagged,
		Changes:    changes,
	}
	if !h.QualityAt.IsZero() {
		at := h.QualityAt
		out.QualityAt = &at
	}
	return out
}
