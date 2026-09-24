package conversation_repository

import (
	"strings"
	"testing"

	"vozko/domain/shared"
)

func TestUnofficialReadersUseTheStoredCampaign(t *testing.T) {
	// The inbox row, the automation profile the roulette reads, and the entry
	// sources must all take the campaign stored on the conversation. Searching
	// the entries left a window in which a campaign's new conversation followed
	// the instance's AI.
	ch, ok := channelQueryFor(shared.EntryTypeUnofficialWhatsApp)
	if !ok {
		t.Fatal("no unofficial channel query")
	}
	if !strings.Contains(ch.EntryJoin, UnofficialCampaignJoin("uwc", "camp")) {
		t.Errorf("the inbox join does not use the stored campaign:\n%s", ch.EntryJoin)
	}
	if strings.Contains(ch.EntryJoin, "campaign_entries") {
		t.Error("the inbox join still searches campaign entries")
	}
	for _, src := range entrySources {
		if src.EntryType != shared.EntryTypeUnofficialWhatsApp {
			continue
		}
		if strings.Contains(src.CampaignID, "campaign_entries") || !strings.Contains(src.CampaignID, "uwc.campaign_id") {
			t.Errorf("the entry source campaign does not read the stored campaign: %s", src.CampaignID)
		}
	}
}
