package carriercampaign_usecase

import (
	"context"
	"errors"
	"reflect"
	"testing"

	audience "vozko/domain/campaignaudience"
	cc "vozko/domain/carriercampaign"
	"vozko/domain/lead"
	campaignaudience_usecase "vozko/usecases/campaignaudience"
)

func TestEstimateCountsTheAudienceWithTheCarrierRulesAndKeepsThePriceOnTheSelection(t *testing.T) {
	h := newHarness(t)
	h.addLead("lead-a", 1)
	h.addLead("lead-b", 2, func(l *lead.Lead) { l.Blocked = true })
	h.addLead("lead-c", 3, func(l *lead.Lead) { l.Number = "" })
	h.addLead("lead-d", 4)
	h.suppressed["5511999990004"] = true
	h.campaigns.put(rcsCampaign("c1"))

	got, err := h.svc.Estimate(context.Background(), wsID, "c1", Actor{UserID: owner})
	if err != nil {
		t.Fatal(err)
	}
	if got.Audience != 4 || got.Estimate.Messages != 4 {
		t.Fatalf("the price stays on the selected leads: audience %d messages %d", got.Audience, got.Estimate.Messages)
	}
	if got.Count == nil || got.Count.Sendable == nil || *got.Count.Sendable != 1 || got.Count.WithContact != 3 {
		t.Fatalf("count = %+v", got.Count)
	}
	want := map[string]int{string(cc.SkipBlocked): 1, string(cc.SkipNoPhone): 1, string(cc.SkipSuppressed): 1}
	if !reflect.DeepEqual(got.Count.Out, want) {
		t.Fatalf("out = %+v", got.Count.Out)
	}
}

func TestTheCarrierAudienceTakesNoListAndNeedsAKind(t *testing.T) {
	h := newHarness(t)
	ch := h.svc.AudienceChannel()
	if _, err := ch.Prepare(context.Background(), campaignaudience_usecase.Actor{WorkspaceID: wsID}, campaignaudience_usecase.Params{}); !errors.Is(err, audience.ErrKindRequired) {
		t.Fatalf("no kind = %v", err)
	}
	prepared, err := ch.Prepare(context.Background(), campaignaudience_usecase.Actor{WorkspaceID: wsID}, campaignaudience_usecase.Params{Kind: string(cc.KindMarketing)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.Open(context.Background(), nil, audience.SourceList); !errors.Is(err, audience.ErrSourceUnsupported) {
		t.Fatalf("list = %v", err)
	}
	h.svc.SetAudience(nil)
	h.campaigns.put(rcsCampaign("c1"))
	h.addLead("lead-a", 1)
	if _, err := h.svc.Estimate(context.Background(), wsID, "c1", Actor{UserID: owner}); !errors.Is(err, audience.ErrUnavailable) {
		t.Fatalf("an estimate without the audience step = %v", err)
	}
}
