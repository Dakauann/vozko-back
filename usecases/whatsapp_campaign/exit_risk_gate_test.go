package whatsapp_campaign_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/buttonaction"
	"vozko/domain/whatsapp/template"
)

type admittingGate struct{}

func (admittingGate) AdmitCampaign(_ context.Context, _ string, tmpl *template.Template, _ string) (buttonaction.CampaignAdmission, error) {
	return buttonaction.CampaignAdmission{Template: buttonaction.RefOf(tmpl)}, nil
}

func (admittingGate) RecordCampaignAdmission(context.Context, string, string, buttonaction.CampaignAdmission) error {
	return nil
}

type recordingGate struct {
	err            error
	recordErr      error
	campaignID     string
	templateID     string
	acknowledgedBy string
	recorded       []buttonaction.CampaignAdmission
	storedWhenSeen int
	stored         func() int
}

func (g *recordingGate) AdmitCampaign(_ context.Context, _ string, tmpl *template.Template, acknowledgedBy string) (buttonaction.CampaignAdmission, error) {
	g.templateID, g.acknowledgedBy = tmpl.ID, acknowledgedBy
	if g.err != nil {
		return buttonaction.CampaignAdmission{}, g.err
	}
	return buttonaction.CampaignAdmission{Template: buttonaction.RefOf(tmpl)}, nil
}

func (g *recordingGate) RecordCampaignAdmission(_ context.Context, _ string, campaignID string, admission buttonaction.CampaignAdmission) error {
	g.campaignID = campaignID
	g.recorded = append(g.recorded, admission)
	if g.stored != nil {
		g.storedWhenSeen = g.stored()
	}
	return g.recordErr
}

func TestCreateAsksTheExitRiskGateWithTheAcknowledgement(t *testing.T) {
	f := newCreateFixture()
	uc := f.useCase(f.screener)
	gate := &recordingGate{}
	uc.(*createCampaignUseCase).SetExitRiskGate(gate)

	in := selectionCampaign()
	in.ExitRiskAcknowledgedBy = "user-1"
	created, err := uc.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gate.campaignID != created.ID || gate.templateID != "tmpl-1" || gate.acknowledgedBy != "user-1" {
		t.Fatalf("gate = %+v", gate)
	}
}

func TestCreateRefusedByTheExitRiskGateStoresNothing(t *testing.T) {
	f := newCreateFixture()
	uc := f.useCase(f.screener)
	uc.(*createCampaignUseCase).SetExitRiskGate(&recordingGate{err: buttonaction.ErrExitRiskNotAcknowledged})

	if _, err := uc.Execute(context.Background(), selectionCampaign()); !errors.Is(err, buttonaction.ErrExitRiskNotAcknowledged) {
		t.Fatalf("got %v, want ErrExitRiskNotAcknowledged", err)
	}
	if len(f.campaigns.campaigns) != 0 {
		t.Fatalf("a refused campaign must not be stored: %+v", f.campaigns.campaigns)
	}
}

func TestCreateWithoutAnExitRiskGateIsRefused(t *testing.T) {
	f := newCreateFixture()
	uc := f.useCase(f.screener)
	uc.(*createCampaignUseCase).SetExitRiskGate(nil)
	if _, err := uc.Execute(context.Background(), selectionCampaign()); !errors.Is(err, buttonaction.ErrUnavailable) {
		t.Fatalf("got %v, want ErrUnavailable", err)
	}
}
