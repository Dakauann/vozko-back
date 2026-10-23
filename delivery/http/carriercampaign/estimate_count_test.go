package carriercampaign

import (
	"net/http"
	"testing"

	"vozko/domain/campaignaudience"
	cc "vozko/domain/carriercampaign"
	cm "vozko/domain/carriermessaging"
	ccuc "vozko/usecases/carriercampaign"
)

func TestEstimateResponseCarriesTheSharedAudienceCount(t *testing.T) {
	sendable := 7
	api := &fakeAPI{estimate: ccuc.EstimateResult{
		Audience: 10,
		Estimate: cc.Estimate{Channel: cm.ChannelRCS, Messages: 10},
		Count:    &campaignaudience.Count{Channel: campaignaudience.ChannelCarrier, Source: campaignaudience.SourceLeads, Matched: 10, WithContact: 9, Sendable: &sendable, Out: map[string]int{"no_phone": 1, "blocked": 2}, OutTotal: 3},
	}}
	rec, body := serve(t, api, http.MethodPost, "/carrier/campaigns/c1/estimate", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	count, ok := body["count"].(map[string]any)
	if !ok {
		t.Fatalf("count missing in %v", body)
	}
	out := count["out"].(map[string]any)
	if count["matched"] != float64(10) || count["withContact"] != float64(9) || count["sendable"] != float64(7) || out["blocked"] != float64(2) || count["outTotal"] != float64(3) {
		t.Fatalf("count = %v", count)
	}
}
