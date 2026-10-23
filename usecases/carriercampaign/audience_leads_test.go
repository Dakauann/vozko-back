package carriercampaign_usecase

import (
	"context"
	"testing"

	audience "vozko/domain/campaignaudience"
	cc "vozko/domain/carriercampaign"
	"vozko/domain/lead"
	"vozko/domain/selection"
	campaignaudience_usecase "vozko/usecases/campaignaudience"
)

func TestTheCarrierAudienceLeadsMatchTheCountUnderEachReason(t *testing.T) {
	h := newHarness(t)
	h.addLead("lead-a", 1)
	h.addLead("lead-b", 2, func(l *lead.Lead) { l.Blocked = true })
	h.addLead("lead-c", 3, func(l *lead.Lead) { l.Number = "" })
	h.addLead("lead-d", 4)
	h.suppressed["5511999990004"] = true
	actor := campaignaudience_usecase.Actor{WorkspaceID: wsID, UserID: owner}
	params := campaignaudience_usecase.Params{Kind: string(cc.KindMarketing)}
	picked := selection.Selection{Mode: selection.ModeIDs, IDs: []string{"lead-a", "lead-b", "lead-c", "lead-d"}}
	spec := audience.Spec{Source: audience.SourceLeads, Selection: &picked}
	count, err := h.svc.audience.Count(context.Background(), actor, audience.ChannelCarrier, params, spec)
	if err != nil {
		t.Fatal(err)
	}
	filters := map[string]audience.LeadFilter{
		string(cc.SkipBlocked):    {Reason: string(cc.SkipBlocked)},
		string(cc.SkipNoPhone):    {Reason: string(cc.SkipNoPhone)},
		string(cc.SkipSuppressed): {Reason: string(cc.SkipSuppressed)},
	}
	for reason, filter := range filters {
		page, err := h.svc.audience.Leads(context.Background(), actor, audience.ChannelCarrier, params, spec, campaignaudience_usecase.LeadsQuery{Filter: filter, Page: 1, Limit: 50})
		if err != nil || page.Total != count.Out[reason] || page.Total != 1 || page.Items[0].Reason != reason {
			t.Fatalf("%s = %+v, count %d (%v)", reason, page, count.Out[reason], err)
		}
	}
	ready, err := h.svc.audience.Leads(context.Background(), actor, audience.ChannelCarrier, params, spec, campaignaudience_usecase.LeadsQuery{Filter: audience.LeadFilter{Status: audience.LeadReady}, Page: 1, Limit: 50})
	if err != nil || ready.Total != *count.Sendable || ready.Items[0].LeadID != "lead-a" {
		t.Fatalf("ready = %+v, %v", ready, err)
	}
}
