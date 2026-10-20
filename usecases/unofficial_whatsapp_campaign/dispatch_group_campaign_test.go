package unofficial_whatsapp_campaign

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/campaign"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

func seedStoppedGroupCampaign(campaigns *fakeCampaignRepo, entries *fakeEntryRepo, pool ...string) {
	campaigns.put(&uwc.Campaign{
		ID: "camp-g", WorkspaceID: "ws-1", SenderInstanceIDs: pool, Audience: uwc.AudienceGroups,
		Name: "g", Status: campaign.StatusStopped,
		Message: uwc.MessageSpec{Kind: uwc.KindText, Bodies: []string{"oferta"}},
	})
	entry := uwc.NewGroupEntry("camp-g", "ws-1", groupJID, "Ofertas VIP #1", time.Now())
	entry.ID = "entry-g"
	entries.put(&entry)
}

func TestAGroupCampaignStartsWhenOneOfItsNumbersCanSend(t *testing.T) {
	uc, campaigns, entries, gateway, consumer := newDispatchHarness(t)
	gateway.byID = map[string]*uw.Instance{
		"inst-1": number("inst-1", uw.StatusDisconnected),
		"inst-2": number("inst-2", uw.StatusConnected),
	}
	seedStoppedGroupCampaign(campaigns, entries, "inst-1", "inst-2")

	if err := uc.Dispatch(context.Background(), uwc.DispatchCampaignInput{CampaignID: "camp-g", Action: campaign.ActionStart}); err != nil {
		t.Fatalf("Dispatch = %v, want the campaign started through inst-2", err)
	}
	if !consumer.subscribed["camp-g"] {
		t.Fatalf("the consumer was not subscribed")
	}
}

func TestAGroupCampaignDoesNotStartWhenNoneOfItsNumbersCanSend(t *testing.T) {
	uc, campaigns, entries, gateway, consumer := newDispatchHarness(t)
	gateway.byID = map[string]*uw.Instance{"inst-1": number("inst-1", uw.StatusDisconnected)}
	seedStoppedGroupCampaign(campaigns, entries, "inst-1", "inst-gone")

	err := uc.Dispatch(context.Background(), uwc.DispatchCampaignInput{CampaignID: "camp-g", Action: campaign.ActionStart})

	if !errors.Is(err, uw.ErrInstanceNotConnected) {
		t.Fatalf("Dispatch = %v, want the offline number reported", err)
	}
	if consumer.subscribed["camp-g"] {
		t.Fatalf("a campaign without a usable number was subscribed")
	}
}

func TestAGroupCampaignWithAnEmptyPoolDoesNotStart(t *testing.T) {
	uc, campaigns, entries, _, _ := newDispatchHarness(t)
	seedStoppedGroupCampaign(campaigns, entries)

	err := uc.Dispatch(context.Background(), uwc.DispatchCampaignInput{CampaignID: "camp-g", Action: campaign.ActionStart})

	if !errors.Is(err, uwc.ErrGroupCampaignNoNumber) {
		t.Fatalf("Dispatch = %v, want ErrGroupCampaignNoNumber", err)
	}
}
