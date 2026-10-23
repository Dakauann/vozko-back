package smartcampaign_usecase

import (
	"context"
	"fmt"
	"strings"

	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/quality"
	"vozko/domain/whatsapp/template"
	sc "vozko/domain/whatsapp_campaign/smartcampaign"
)

type TemplateSource interface {
	FindByID(id string) (*template.Template, error)
}

type PhoneSource interface {
	FindByID(id string) (*businessphone.WhatsAppBusinessPhoneNumber, error)
}

type PhoneProbe interface {
	RefreshPhone(ctx context.Context, phone *businessphone.WhatsAppBusinessPhoneNumber) (businessphone.MetaPhoneNumberInfo, error)
}

type HealthReader struct {
	templates TemplateSource
	phones    PhoneSource
	timeline  quality.Timeline
	probe     PhoneProbe
}

func NewHealthReader(templates TemplateSource, phones PhoneSource, timeline quality.Timeline, probe PhoneProbe) *HealthReader {
	return &HealthReader{templates: templates, phones: phones, timeline: timeline, probe: probe}
}

var _ sc.HealthReader = (*HealthReader)(nil)

func (h *HealthReader) Template(_ context.Context, templateID string) (sc.TemplateState, error) {
	tmpl, err := h.templates.FindByID(templateID)
	if err != nil {
		return sc.TemplateState{}, fmt.Errorf("read template %s: %w", templateID, err)
	}
	if tmpl == nil {
		return sc.TemplateState{}, fmt.Errorf("template %s not found", templateID)
	}
	latest, err := h.timeline.LatestBySignal(quality.SubjectTemplate, tmpl.ExternalID)
	if err != nil {
		return sc.TemplateState{}, err
	}
	reading := quality.ReadTemplate(latest)
	return sc.TemplateState{
		Status:     tmpl.Status,
		StatusAt:   reading.StatusAt,
		Flagged:    reading.Flagged,
		Quality:    businessphone.QualityRating(reading.Quality),
		QualityAt:  reading.QualityAt,
		Category:   tmpl.Category,
		CategoryAt: reading.CategoryAt,
	}, nil
}

func (h *HealthReader) Phone(ctx context.Context, phoneID string, fresh bool) (sc.PhoneState, error) {
	phone, err := h.phones.FindByID(phoneID)
	if err != nil {
		return sc.PhoneState{}, fmt.Errorf("read phone %s: %w", phoneID, err)
	}
	if phone == nil {
		return sc.PhoneState{}, fmt.Errorf("phone %s not found", phoneID)
	}
	state := sc.PhoneState{Status: phone.Status, Quality: phone.QualityRating}
	if fresh && phone.QualityPollable() {
		info, err := h.probe.RefreshPhone(ctx, phone)
		if err != nil {
			return sc.PhoneState{}, err
		}
		if status := upper(info.Status); status != "" {
			state.Status = businessphone.Status(status)
		}
		if rating := upper(info.QualityRating); rating != "" {
			state.Quality = businessphone.QualityRating(rating)
		}
	}
	latest, err := h.timeline.LatestBySignal(quality.SubjectPhone, phone.MetaPhoneNumberID)
	if err != nil {
		return sc.PhoneState{}, err
	}
	if change, ok := latest[quality.SignalStatus]; ok {
		state.ChangedAt = change.OccurredAt
	}
	if change, ok := latest[quality.SignalQuality]; ok {
		state.QualityAt = change.OccurredAt
	}
	return state, nil
}

func upper(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}
