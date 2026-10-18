package unofficial_whatsapp_campaign

import (
	"context"
	"errors"
	"testing"

	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

func updateWithAutomation(campaigns *fakeCampaignRepo, entries *fakeEntryRepo, gateway *fakeGateway) uwc.UpdateCampaignUseCase {
	uc := NewUpdateCampaignUseCase(campaigns, entries, gateway)
	uc.(*updateCampaignUseCase).SetAutomation(automationThatAllows{})
	return uc
}

func TestACampaignCreatedBeforeTheExitRuleKeepsItsMessageThroughAnEdit(t *testing.T) {
	campaigns, entries, gateway := stoppedUnofficial("")
	renamed := edited("Olá {{1}}")
	renamed.Name = "Matrículas outubro"

	saved, err := updateWithAutomation(campaigns, entries, gateway).Execute(context.Background(), "camp-1", renamed, uw.Unrestricted())

	if err != nil || saved.Name != "Matrículas outubro" {
		t.Fatalf("Execute = %+v, %v", saved, err)
	}
}

func TestANewMessageOnAnEditMustShowTheExit(t *testing.T) {
	campaigns, entries, gateway := stoppedUnofficial("")
	uc := updateWithAutomation(campaigns, entries, gateway)

	if _, err := uc.Execute(context.Background(), "camp-1", edited("Oi {{1}}"), uw.Unrestricted()); !errors.Is(err, uwc.ErrCampaignNeedsExit) {
		t.Fatalf("Execute = %v, want ErrCampaignNeedsExit", err)
	}
	if got, _ := campaigns.FindByID("camp-1"); got.Message.Bodies[0] != "Olá {{1}}" {
		t.Fatalf("a refused message was saved: %+v", got.Message)
	}

	if _, err := uc.Execute(context.Background(), "camp-1", edited("Oi {{1}}, responda /sair para sair"), uw.Unrestricted()); err != nil {
		t.Fatalf("Execute with the exit = %v", err)
	}
}
