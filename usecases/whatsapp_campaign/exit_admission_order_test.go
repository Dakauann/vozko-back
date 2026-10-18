package whatsapp_campaign_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/campaign"
	wc "vozko/domain/whatsapp_campaign"
)

func TestTheAcknowledgementIsRecordedOnlyAfterTheCampaignIsSaved(t *testing.T) {
	f := newCreateFixture()
	uc := f.useCase(f.screener)
	gate := &recordingGate{stored: func() int { return len(f.campaigns.campaigns) }}
	uc.(*createCampaignUseCase).SetExitRiskGate(gate)

	in := selectionCampaign()
	in.ExitRiskAcknowledgedBy = "user-1"
	created, err := uc.Execute(context.Background(), in)

	if err != nil {
		t.Fatal(err)
	}
	if len(gate.recorded) != 1 || gate.campaignID != created.ID || gate.storedWhenSeen != 1 {
		t.Fatalf("recorded %d for %q with %d campaigns stored", len(gate.recorded), gate.campaignID, gate.storedWhenSeen)
	}
}

func TestACreateThatFailsAfterTheGateRecordsNoAcknowledgement(t *testing.T) {
	f := newCreateFixture()
	uc := f.useCase(&fakeScreener{skip: map[string]campaign.SkipReason{}, err: errors.New("screening down")})
	gate := &recordingGate{}
	uc.(*createCampaignUseCase).SetExitRiskGate(gate)

	in := selectionCampaign()
	in.ExitRiskAcknowledgedBy = "user-1"
	if _, err := uc.Execute(context.Background(), in); err == nil {
		t.Fatal("want the screening failure")
	}
	if len(gate.recorded) != 0 {
		t.Fatalf("an orphan acknowledgement was recorded: %+v", gate.recorded)
	}
}

func TestAFailedRecordAfterTheSaveIsReported(t *testing.T) {
	f := newCreateFixture()
	uc := f.useCase(f.screener)
	uc.(*createCampaignUseCase).SetExitRiskGate(&recordingGate{recordErr: errors.New("routes store down")})

	if _, err := uc.Execute(context.Background(), selectionCampaign()); err == nil {
		t.Fatal("a lost acknowledgement must not pass silently")
	}
}

func TestATemplateChangeRecordsTheNewTemplatesAdmissionAfterTheUpdate(t *testing.T) {
	stored, fixture := updateFixture(t, "")
	counted := &countedCampaigns{mockCampaignRepo: stored}
	fixtureUC := fixture.(*updateCampaignUseCase)
	uc := NewUpdateCampaignUseCase(counted, fixtureUC.entryRepo, fixtureUC.templateRepo, fixtureUC.businessPhoneRepo, nil)
	uc.(*updateCampaignUseCase).SetTemplateGrants(allowGrants{})
	gate := &recordingGate{stored: func() int { return counted.updates }}
	uc.(*updateCampaignUseCase).SetExitRiskGate(gate)

	if _, err := uc.Execute(context.Background(), "camp-1", &wc.Campaign{Name: "Matrículas", TemplateID: "tmpl-2"}); err != nil {
		t.Fatal(err)
	}
	if len(gate.recorded) != 1 || gate.campaignID != "camp-1" || gate.storedWhenSeen != 1 {
		t.Fatalf("recorded %+v for %q after %d updates", gate.recorded, gate.campaignID, gate.storedWhenSeen)
	}
}
