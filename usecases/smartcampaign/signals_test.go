package smartcampaign_usecase

import (
	"context"
	"testing"

	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/template"
)

type healthListener struct{ calls [][2]string }

func (h *healthListener) HealthChanged(_ context.Context, templateID, phoneID string) error {
	h.calls = append(h.calls, [2]string{templateID, phoneID})
	return nil
}

type templatesByExternal map[string]*template.Template

func (t templatesByExternal) FindByExternalID(externalID string) (*template.Template, error) {
	return t[externalID], nil
}

type phoneRepo struct {
	businessphone.Repository
	updateErr error
	updated   []string
}

func (p *phoneRepo) Update(id string, _ *businessphone.WhatsAppBusinessPhoneNumber) error {
	if p.updateErr != nil {
		return p.updateErr
	}
	p.updated = append(p.updated, id)
	return nil
}

func TestATemplateSignalReachesTheEngineWithTheInternalID(t *testing.T) {
	listener := &healthListener{}
	signals := NewSignals(templatesByExternal{"x1": {ID: "tpl-1", ExternalID: "x1"}}, listener)

	signals.TemplateChanged("x1")
	signals.TemplateChanged("unknown")

	if len(listener.calls) != 1 || listener.calls[0] != [2]string{"tpl-1", ""} {
		t.Fatalf("calls = %v, want only the known template", listener.calls)
	}
}

func TestASavedPhoneIsSignalledAndAFailedSaveIsNot(t *testing.T) {
	listener := &healthListener{}
	repo := &phoneRepo{}
	phones := SignalPhoneChanges(repo, NewSignals(templatesByExternal{}, listener))

	if err := phones.Update("phone-1", &businessphone.WhatsAppBusinessPhoneNumber{}); err != nil {
		t.Fatal(err)
	}
	repo.updateErr = errBroken
	if err := phones.Update("phone-2", &businessphone.WhatsAppBusinessPhoneNumber{}); err == nil {
		t.Fatal("the save error was swallowed")
	}

	if len(listener.calls) != 1 || listener.calls[0] != [2]string{"", "phone-1"} {
		t.Fatalf("calls = %v, want only the saved phone", listener.calls)
	}
}
