package carriercampaign_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	cc "vozko/domain/carriercampaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/rcs"
)

func TestFunnel(t *testing.T) {
	h := newHarness(t)
	h.campaigns.put(rcsCampaign("c1"))
	h.funnels.funnel = cc.Funnel{Sent: 10, Delivered: 8}
	got, err := h.svc.Funnel(context.Background(), wsID, "c1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Funnel.Sent != 10 || got.Rates.Delivery == nil || *got.Rates.Delivery != 0.8 {
		t.Fatalf("funnel %+v", got)
	}
	q := h.funnels.queries[0]
	if q != (cc.FunnelQuery{WorkspaceID: wsID, Channel: carriermessaging.ChannelRCS, CampaignID: "c1"}) {
		t.Fatalf("query %+v", q)
	}
	if _, err := h.svc.Funnel(context.Background(), "ws2", "c1"); !errors.Is(err, cc.ErrCampaignNotFound) {
		t.Fatalf("foreign err = %v", err)
	}
}

func TestRunFunnel(t *testing.T) {
	h := newHarness(t)
	h.runningRun(rcsCampaign("c1"), "run-1")
	other := rcsCampaign("c2")
	h.runningRun(other, "run-2")
	if _, err := h.svc.RunFunnel(context.Background(), wsID, "c1", "run-1"); err != nil {
		t.Fatal(err)
	}
	if h.funnels.queries[0].RunID != "run-1" || h.funnels.queries[0].CampaignID != "c1" {
		t.Fatalf("query %+v", h.funnels.queries[0])
	}
	if _, err := h.svc.RunFunnel(context.Background(), wsID, "c1", "run-2"); !errors.Is(err, cc.ErrRunNotFound) {
		t.Fatalf("foreign run err = %v", err)
	}
}

func TestVariants(t *testing.T) {
	h := newHarness(t)
	c := rcsCampaign("c1")
	c.Variants = append(c.Variants, c.Variants[0])
	c.Variants[0].Name, c.Variants[1].Name = "A", "B"
	h.campaigns.put(c)
	h.funnels.variants = map[int]cc.Funnel{1: {Sent: 4, Delivered: 2}}
	got, err := h.svc.Variants(context.Background(), wsID, "c1")
	if err != nil || len(got) != 2 || got[1].Name != "B" || got[1].Funnel.Sent != 4 || got[0].Funnel.Sent != 0 {
		t.Fatalf("variants %+v %v", got, err)
	}
}

func TestButtons(t *testing.T) {
	h := newHarness(t)
	c := rcsCampaign("c1")
	c.Variants[0].Content.Suggestions = []rcs.Suggestion{{Kind: rcs.SuggestReply, Label: "Quero"}}
	h.campaigns.put(c)
	h.campaigns.put(smsCampaign("c2"))
	h.funnels.taps = map[rcs.ButtonKey]int64{{Card: -1, Index: 0}: 3}

	got, err := h.svc.Buttons(context.Background(), wsID, "c1", 0)
	if err != nil || len(got) != 1 || got[0].Label != "Quero" || got[0].Taps != 3 {
		t.Fatalf("buttons %+v %v", got, err)
	}
	if _, err := h.svc.Buttons(context.Background(), wsID, "c1", 2); !errors.Is(err, ErrVariantIndex) {
		t.Fatalf("variant err = %v", err)
	}
	sms, err := h.svc.Buttons(context.Background(), wsID, "c2", 0)
	if err != nil || len(sms) != 0 {
		t.Fatalf("sms buttons %+v %v", sms, err)
	}
}

func TestCarriers(t *testing.T) {
	h := newHarness(t)
	h.campaigns.put(rcsCampaign("c1"))
	h.funnels.carriers = []cc.CarrierStat{{Operator: "Vivo", Sent: 10, FellBack: 2}}
	got, err := h.svc.Carriers(context.Background(), wsID, "c1")
	if err != nil || len(got) != 1 || got[0].Operator != "Vivo" {
		t.Fatalf("carriers %+v %v", got, err)
	}
}

func TestAttribute(t *testing.T) {
	h := newHarness(t)
	won := testNow
	h.funnels.touches = []cc.Touch{
		{MessageID: "m1", CampaignID: "c1", DeliveredAt: won.Add(-10 * 24 * time.Hour)},
		{MessageID: "m2", CampaignID: "c1", DeliveredAt: won.Add(-3 * 24 * time.Hour)},
		{MessageID: "m3", CampaignID: "c2", DeliveredAt: won.Add(-24 * time.Hour)},
		{MessageID: "m4", CampaignID: "c3", DeliveredAt: won.Add(time.Hour)},
	}
	got, ok, err := h.svc.Attribute(context.Background(), wsID, "lead-a", won)
	if err != nil || !ok || got.MessageID != "m3" {
		t.Fatalf("touch %+v %v %v", got, ok, err)
	}
	if !h.funnels.since.Equal(won.Add(-7 * 24 * time.Hour)) {
		t.Fatalf("since %s", h.funnels.since)
	}
	h.funnels.touches = h.funnels.touches[:1]
	if _, ok, err := h.svc.Attribute(context.Background(), wsID, "lead-a", won); err != nil || ok {
		t.Fatalf("old touch counted: %v %v", ok, err)
	}
}
