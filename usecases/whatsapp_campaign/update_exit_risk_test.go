package whatsapp_campaign_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/buttonaction"
	wc "vozko/domain/whatsapp_campaign"
)

func TestChangingTheTemplateGoesThroughTheExitRiskGate(t *testing.T) {
	campaigns, uc := updateFixture(t, "")
	gate := &recordingGate{}
	uc.(*updateCampaignUseCase).SetExitRiskGate(gate)

	in := &wc.Campaign{Name: "Matrículas", TemplateID: "tmpl-2", ExitRiskAcknowledgedBy: "user-1"}
	if _, err := uc.Execute(context.Background(), "camp-1", in); err != nil {
		t.Fatalf("Execute = %v", err)
	}
	if gate.campaignID != "camp-1" || gate.templateID != "tmpl-2" || gate.acknowledgedBy != "user-1" {
		t.Fatalf("gate = %+v", gate)
	}
	if campaigns.campaigns["camp-1"].TemplateID != "tmpl-2" {
		t.Fatal("the admitted template was not saved")
	}
}

type countedCampaigns struct {
	*mockCampaignRepo
	updates int
}

func (c *countedCampaigns) FindByID(id string) (*wc.Campaign, error) {
	found, err := c.mockCampaignRepo.FindByID(id)
	if err != nil {
		return nil, err
	}
	copied := *found
	return &copied, nil
}

func (c *countedCampaigns) Update(id string, campaign *wc.Campaign) error {
	c.updates++
	return c.mockCampaignRepo.Update(id, campaign)
}

func TestATemplateChangeRefusedByTheGateSavesNothing(t *testing.T) {
	stored, fixture := updateFixture(t, "")
	counted := &countedCampaigns{mockCampaignRepo: stored}
	fixtureUC := fixture.(*updateCampaignUseCase)
	uc := NewUpdateCampaignUseCase(counted, fixtureUC.entryRepo, fixtureUC.templateRepo, fixtureUC.businessPhoneRepo, nil)
	uc.(*updateCampaignUseCase).SetTemplateGrants(allowGrants{})
	uc.(*updateCampaignUseCase).SetExitRiskGate(&recordingGate{err: buttonaction.ErrExitRiskNotAcknowledged})

	_, err := uc.Execute(context.Background(), "camp-1", &wc.Campaign{Name: "Outro nome", TemplateID: "tmpl-2"})
	if !errors.Is(err, buttonaction.ErrExitRiskNotAcknowledged) {
		t.Fatalf("Execute = %v, want ErrExitRiskNotAcknowledged", err)
	}
	if counted.updates != 0 || stored.campaigns["camp-1"].TemplateID != "tmpl-1" {
		t.Fatalf("a refused change was saved: %d updates, %+v", counted.updates, stored.campaigns["camp-1"])
	}
}

func TestKeepingTheTemplateDoesNotAskTheGate(t *testing.T) {
	_, uc := updateFixture(t, "")
	gate := &recordingGate{err: buttonaction.ErrExitRiskNotAcknowledged}
	uc.(*updateCampaignUseCase).SetExitRiskGate(gate)

	for _, in := range []*wc.Campaign{{Name: "Novo nome"}, {Name: "Novo nome", TemplateID: "tmpl-1"}} {
		if _, err := uc.Execute(context.Background(), "camp-1", in); err != nil {
			t.Fatalf("Execute(%+v) = %v", in, err)
		}
	}
	if gate.campaignID != "" {
		t.Fatalf("the gate was asked without a template change: %+v", gate)
	}
}

func TestATemplateChangeWithoutTheGateIsRefused(t *testing.T) {
	_, uc := updateFixture(t, "")
	if _, err := uc.Execute(context.Background(), "camp-1", &wc.Campaign{Name: "Matrículas", TemplateID: "tmpl-2"}); !errors.Is(err, buttonaction.ErrUnavailable) {
		t.Fatalf("Execute = %v, want ErrUnavailable", err)
	}
}
