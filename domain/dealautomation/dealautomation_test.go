package dealautomation

import (
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workspace"
)

func TestEachChannelIsGovernedByItsOwnPermission(t *testing.T) {
	cases := map[Channel]workspace.Resource{
		{EntryType: shared.EntryTypeWhatsApp, Kind: conversation.ContainerKindCampaign}:           workspace.ResourceWhatsAppCampaigns,
		{EntryType: shared.EntryTypeUnofficialWhatsApp, Kind: conversation.ContainerKindAccount}:  workspace.ResourceUnofficialWhatsAppInstances,
		{EntryType: shared.EntryTypeUnofficialWhatsApp, Kind: conversation.ContainerKindCampaign}: workspace.ResourceUnofficialWhatsAppCampaigns,
		{EntryType: shared.EntryTypeInstagram, Kind: conversation.ContainerKindAccount}:           workspace.ResourceInstagramAccounts,
		{EntryType: shared.EntryTypeFacebook, Kind: conversation.ContainerKindAccount}:            workspace.ResourceFacebookPages,
		{EntryType: shared.EntryTypeTelegram, Kind: conversation.ContainerKindAccount}:            workspace.ResourceTelegramAccounts,
	}
	for channel, want := range cases {
		got, ok := channel.Resource()
		if !ok || got != want {
			t.Errorf("%+v: got %q, %v", channel, got, ok)
		}
	}
}

func TestUnknownChannelsAreNotSupported(t *testing.T) {
	for _, channel := range []Channel{
		{EntryType: shared.EntryTypeWhatsApp, Kind: conversation.ContainerKindAccount},
		{EntryType: shared.EntryTypeInstagram, Kind: conversation.ContainerKindCampaign},
		{EntryType: "voice", Kind: conversation.ContainerKindAccount},
	} {
		if _, ok := channel.Resource(); ok {
			t.Errorf("%+v must not be supported", channel)
		}
	}
}

func TestSettingIsOnOnlyWithAFunnel(t *testing.T) {
	if (Setting{}).Enabled() {
		t.Fatal("no funnel means off")
	}
	if !(Setting{PipelineID: "p1"}).Enabled() {
		t.Fatal("a funnel turns it on")
	}
}
