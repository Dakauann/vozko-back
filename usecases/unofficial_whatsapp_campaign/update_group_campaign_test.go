package unofficial_whatsapp_campaign

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/campaign"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

func stoppedGroupCampaign() (*fakeCampaignRepo, *fakeEntryRepo, *fakeGateway) {
	campaigns := newFakeCampaignRepo()
	campaigns.put(&uwc.Campaign{
		ID: "camp-1", WorkspaceID: "ws-1", InstanceID: "inst-1", Name: "Ofertas", Status: campaign.StatusStopped,
		Audience: uwc.AudienceGroups,
		Message:  uwc.MessageSpec{Kind: uwc.KindText, Bodies: []string{"Oferta de hoje"}},
	})
	gateway := &fakeGateway{instance: &uw.Instance{ID: "inst-1", WorkspaceID: "ws-1", Status: uw.StatusConnected}}
	return campaigns, newFakeEntryRepo(), gateway
}

func TestAGroupCampaignEditKeepsItsNumber(t *testing.T) {
	campaigns, entries, gateway := stoppedGroupCampaign()
	edit := edited("Oferta nova")
	edit.InstanceID = "inst-2"

	_, err := updateWithAutomation(campaigns, entries, gateway).Execute(context.Background(), "camp-1", edit, uw.Unrestricted())

	if !errors.Is(err, uwc.ErrGroupCampaignNumberLocked) {
		t.Fatalf("Execute = %v, want ErrGroupCampaignNumberLocked", err)
	}
}

func TestAGroupCampaignEditChangesTheMessageWithoutAnExitCommand(t *testing.T) {
	campaigns, entries, gateway := stoppedGroupCampaign()

	saved, err := updateWithAutomation(campaigns, entries, gateway).Execute(context.Background(), "camp-1", edited("Oferta nova: arroz 5 kg"), uw.Unrestricted())

	if err != nil || saved.Message.Bodies[0] != "Oferta nova: arroz 5 kg" {
		t.Fatalf("Execute = %+v, %v", saved, err)
	}
	if saved.Audience != uwc.AudienceGroups {
		t.Fatalf("audience after edit = %q, want groups", saved.Audience)
	}
}
