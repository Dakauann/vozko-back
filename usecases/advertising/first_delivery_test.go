package advertising

import (
	"context"
	"testing"

	ads "vozko/domain/advertising"
)

func TestSyncStampsTheFirstDeliveryOfEachObjectThatGotImpressions(t *testing.T) {
	w := newWorld()
	day, _ := ads.ParseDay("2026-09-30")
	w.gateway.insights = []ads.DailyInsight{
		{AdMetaID: "a-1", AdSetMetaID: "s-1", CampaignMetaID: "c-1", Day: day, Currency: "BRL", Impressions: 120},
		{AdMetaID: "a-2", AdSetMetaID: "s-1", CampaignMetaID: "c-1", Day: day, Currency: "BRL", Impressions: 0},
	}
	if _, err := w.sync.Sync(context.Background(), "ws-1", "acc-1"); err != nil {
		t.Fatal(err)
	}
	got := w.objects.delivered
	for _, id := range []string{"a-1", "s-1", "c-1"} {
		if !got[id] {
			t.Fatalf("%s not stamped, stamped %v", id, got)
		}
	}
	if got["a-2"] {
		t.Fatal("an ad without impressions was stamped")
	}
}
