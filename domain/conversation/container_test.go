package conversation

import "testing"

func TestACampaignConversationUsesItsCampaign(t *testing.T) {
	got := ContainerIDOf(InboxEntry{CampaignID: "camp-1", BusinessPhoneID: "inst-1"})
	if got != "camp-1" {
		t.Fatalf("container = %q, want the campaign when there is one", got)
	}
}

func TestAnOrganicConversationFallsBackToItsChannelAccount(t *testing.T) {
	got := ContainerIDOf(InboxEntry{CampaignID: "", BusinessPhoneID: "inst-1"})
	if got != "inst-1" {
		t.Fatalf("container = %q; a number with no campaign must still name its container", got)
	}
}

func TestBlankValuesDoNotCountAsAContainer(t *testing.T) {
	if got := ContainerIDOf(InboxEntry{CampaignID: "   ", BusinessPhoneID: "  "}); got != "" {
		t.Fatalf("container = %q, want empty", got)
	}
}

func TestWhitespaceAroundACampaignIsTrimmed(t *testing.T) {
	if got := ContainerIDOf(InboxEntry{CampaignID: " camp-1 "}); got != "camp-1" {
		t.Fatalf("container = %q", got)
	}
}

func TestAnEntryWithNothingResolvesToNoContainer(t *testing.T) {
	if got := ContainerIDOf(InboxEntry{}); got != "" {
		t.Fatalf("container = %q, want empty so the caller skips it", got)
	}
}
