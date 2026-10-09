package conversation_usecase

import (
	"testing"

	"vozko/domain/lead"
	"vozko/domain/shared"
	wce "vozko/domain/whatsapp_campaign_entry"
)

func TestGetEntryInfo_WhatsAppHeaderCarriesTheLeadBlockState(t *testing.T) {
	for _, blocked := range []bool{true, false} {
		svc := &HistoryProviderService{
			whatsappRepo: &targetEntryRepo{
				entry:    &wce.WhatsAppCampaignEntry{ID: "e1", LeadID: "lead-1"},
				campaign: &wce.EntryCampaignInfo{WorkspaceID: "ws-1"},
			},
			leadRepo: &targetLeadRepo{lead: &lead.Lead{ID: "lead-1", Blocked: blocked, Version: 9}},
		}

		info, err := svc.GetEntryInfo("e1", string(shared.EntryTypeWhatsApp))
		if err != nil {
			t.Fatalf("GetEntryInfo: %v", err)
		}
		if info.Blocked != blocked || info.LeadVersion != 9 {
			t.Fatalf("header blocked = %v at v%d, want %v at v9", info.Blocked, info.LeadVersion, blocked)
		}
	}
}

func TestGetEntryInfo_ContactHeaderCarriesTheLeadBlockState(t *testing.T) {
	svc := &HistoryProviderService{leadRepo: &headerLeadRepo{byID: map[string]*lead.Lead{
		"lead-1": {ID: "lead-1", Name: "Bia", Blocked: true, Version: 4},
	}}}
	svc.SetContactIdentityLookup(shared.EntryTypeUnofficialWhatsApp, &headerContacts{
		ws:      "ws-1",
		contact: ContactDisplay{ContactID: "contact-1", LeadID: "lead-1", Name: "Bia push"},
	})

	info, err := svc.GetEntryInfo("uw-conv-1", string(shared.EntryTypeUnofficialWhatsApp))
	if err != nil {
		t.Fatalf("GetEntryInfo: %v", err)
	}
	if !info.Blocked || info.LeadVersion != 4 {
		t.Fatalf("header blocked = %v at v%d, want true at v4", info.Blocked, info.LeadVersion)
	}
}

func TestGetEntryInfo_ContactWithoutALeadIsNeverBlocked(t *testing.T) {
	svc := &HistoryProviderService{leadRepo: &headerLeadRepo{byID: map[string]*lead.Lead{}}}
	svc.SetContactIdentityLookup(shared.EntryTypeUnofficialWhatsApp, &headerContacts{
		ws:      "ws-1",
		contact: ContactDisplay{ContactID: "group-1", Name: "Grupo", IsGroup: true},
	})

	info, err := svc.GetEntryInfo("uw-conv-2", string(shared.EntryTypeUnofficialWhatsApp))
	if err != nil {
		t.Fatalf("GetEntryInfo: %v", err)
	}
	if info.Blocked || info.LeadID != "" {
		t.Fatalf("a conversation without a lead reported blocked = %v for lead %q", info.Blocked, info.LeadID)
	}
}
