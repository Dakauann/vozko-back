package campaign

import "testing"

func TestAudienceExitsCountWhoTheSendRuleRefuses(t *testing.T) {
	groups := []LeadFactsGroup{
		{Facts: LeadFacts{Found: true, HasIdentity: true}, Leads: 120},
		{Facts: LeadFacts{Found: true, HasIdentity: true, OptedOut: true}, Leads: 30},
		{Facts: LeadFacts{Found: true, HasIdentity: true, Blocked: true}, Leads: 5},
		{Facts: LeadFacts{Found: true, HasIdentity: true, Blocked: true, OptedOut: true}, Leads: 2},
		{Facts: LeadFacts{Found: true, HasIdentity: true, HasConsent: true}, Leads: 9},
	}
	got := TallyAudienceExits(groups)
	if got.OptedOut != 30 || got.Blocked != 7 || got.NotReceiving() != 37 {
		t.Fatalf("exits = %+v, not receiving %d; want 30 opted out, 7 blocked, 37 in all", got, got.NotReceiving())
	}
}

func TestAnAudienceWithNobodyWhoLeftCountsNothing(t *testing.T) {
	if got := TallyAudienceExits(nil); got.NotReceiving() != 0 {
		t.Fatalf("exits = %+v", got)
	}
}
