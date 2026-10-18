package smartcampaign_usecase

import (
	"context"
	"log"

	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/template"
)

type HealthListener interface {
	HealthChanged(ctx context.Context, templateID, phoneID string) error
}

type TemplateLookup interface {
	FindByExternalID(externalID string) (*template.Template, error)
}

type Signals struct {
	templates TemplateLookup
	listener  HealthListener
}

func NewSignals(templates TemplateLookup, listener HealthListener) *Signals {
	return &Signals{templates: templates, listener: listener}
}

func (s *Signals) TemplateChanged(externalID string) {
	tmpl, err := s.templates.FindByExternalID(externalID)
	if err != nil || tmpl == nil {
		log.Printf("[smart-campaign] template %s changed but could not be read: %v", externalID, err)
		return
	}
	s.notify(tmpl.ID, "")
}

func (s *Signals) PhoneChanged(phoneID string) {
	s.notify("", phoneID)
}

func (s *Signals) notify(templateID, phoneID string) {
	if err := s.listener.HealthChanged(context.Background(), templateID, phoneID); err != nil {
		log.Printf("[smart-campaign] reacting to a health change (template %q, phone %q): %v", templateID, phoneID, err)
	}
}

type SignallingPhones struct {
	businessphone.Repository
	signals *Signals
}

func SignalPhoneChanges(phones businessphone.Repository, signals *Signals) *SignallingPhones {
	return &SignallingPhones{Repository: phones, signals: signals}
}

func (s *SignallingPhones) Update(id string, phone *businessphone.WhatsAppBusinessPhoneNumber) error {
	if err := s.Repository.Update(id, phone); err != nil {
		return err
	}
	s.signals.PhoneChanged(id)
	return nil
}
