package carriercampaign_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/campaign"
	cc "vozko/domain/carriercampaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/lead"
	"vozko/domain/rcs"
	"vozko/domain/shared"
	"vozko/domain/sms"
)

func TestEstimate(t *testing.T) {
	h := newHarness(t)
	h.addLead("lead-a", 1)
	h.addLead("lead-b", 2)
	h.campaigns.put(rcsCampaign("c1"))
	h.balance.micros = 1_500_000

	got, err := h.svc.Estimate(context.Background(), wsID, "c1", Actor{UserID: owner})
	if err != nil {
		t.Fatal(err)
	}
	want := cc.Estimate{Channel: carriermessaging.ChannelRCS, Messages: 2, WorstCaseMicros: 20_000, ExpectedMicros: 20_000, PerRecipientMicros: 10_000}
	if got.Audience != 2 || got.Estimate != want {
		t.Fatalf("estimate %+v", got)
	}
	if got.BalanceMicros != 1_500_000 || got.ShortfallMicros != 0 || got.Readiness != (Readiness{Ready: true, Sender: true, Route: true}) {
		t.Fatalf("money and readiness %+v", got)
	}
	if h.selections.scopes[0] != (scopeOf(wsID, Actor{UserID: owner})) {
		t.Fatalf("scope %+v", h.selections.scopes[0])
	}

	h.balance.micros = 500_000
	got, err = h.svc.Estimate(context.Background(), wsID, "c1", Actor{UserID: owner})
	if err != nil {
		t.Fatal(err)
	}
	if got.ShortfallMicros != 20_000+100*10_000-500_000 {
		t.Fatalf("shortfall %d", got.ShortfallMicros)
	}
}

func TestEstimateReadiness(t *testing.T) {
	withFallback := func() cc.Campaign {
		c := rcsCampaign("c1")
		c.Fallback.SMS = &cc.SMSFallback{RouteID: "route-1", Text: "Oi"}
		return c
	}
	pauseSender := func(h *harness) { h.senders["sender-1"].Status = rcs.SenderPaused }
	pauseRoute := func(h *harness) { h.routes["route-1"].Status = sms.RoutePaused }
	cases := []struct {
		name    string
		mutate  func(h *harness)
		c       func() cc.Campaign
		sender  bool
		route   bool
		blocker string
	}{
		{"launched sender", func(h *harness) {}, func() cc.Campaign { return rcsCampaign("c1") }, true, true, ""},
		{"paused sender", pauseSender, func() cc.Campaign { return rcsCampaign("c1") }, false, true, BlockerSenderNotReady},
		{"missing sender", func(h *harness) { delete(h.senders, "sender-1") }, func() cc.Campaign { return rcsCampaign("c1") }, false, true, BlockerSenderNotFound},
		{"active route", func(h *harness) {}, func() cc.Campaign { return smsCampaign("c1") }, true, true, ""},
		{"paused route of an sms campaign", pauseRoute, func() cc.Campaign { return smsCampaign("c1") }, false, false, BlockerRouteNotReady},
		{"missing route", func(h *harness) { delete(h.routes, "route-1") }, func() cc.Campaign { return smsCampaign("c1") }, false, false, BlockerRouteNotFound},
		{"paused fallback route", pauseRoute, withFallback, true, false, BlockerRouteNotReady},
		{"both paused", func(h *harness) { pauseSender(h); pauseRoute(h) }, withFallback, false, false, BlockerSenderNotReady},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.addLead("lead-a", 1)
			tc.mutate(h)
			h.campaigns.put(tc.c())
			got, err := h.svc.Estimate(context.Background(), wsID, "c1", Actor{UserID: owner})
			if err != nil {
				t.Fatal(err)
			}
			want := Readiness{Ready: tc.blocker == "", Sender: tc.sender, Route: tc.route, Blocker: tc.blocker}
			if got.Readiness != want {
				t.Fatalf("readiness %+v, want %+v", got.Readiness, want)
			}
		})
	}
}

func TestEstimateSMS(t *testing.T) {
	h := newHarness(t)
	h.addLead("lead-a", 1)
	h.campaigns.put(smsCampaign("c1"))
	got, err := h.svc.Estimate(context.Background(), wsID, "c1", Actor{UserID: owner})
	if err != nil {
		t.Fatal(err)
	}
	if got.Estimate.Channel != carriermessaging.ChannelSMS || got.Estimate.WorstCaseSegments != 1 || got.Estimate.WorstCaseMicros != 40_000 {
		t.Fatalf("sms estimate %+v", got.Estimate)
	}
}

func TestEstimateNeedsTheAudience(t *testing.T) {
	h := newHarness(t)
	c := rcsCampaign("c1")
	c.Audience.Mode, c.Audience.IDs = "", nil
	h.campaigns.put(c)
	if _, err := h.svc.Estimate(context.Background(), wsID, "c1", Actor{UserID: owner}); !errors.Is(err, cc.ErrAudienceRequired) {
		t.Fatalf("err = %v", err)
	}
}

func TestTestSend(t *testing.T) {
	h := newHarness(t)
	h.addLead("lead-a", 1, func(l *lead.Lead) { l.Name = "Beatriz Lima" })
	c := rcsCampaign("c1")
	c.Variants = append(c.Variants, cc.Variant{Name: "B", Content: rcs.Content{Text: "Olá {{1}}"}, Bindings: c.Variants[0].Bindings})
	c.Variants[0].Name = "A"
	c.Fallback.SMS = &cc.SMSFallback{RouteID: "route-1", Text: "Oi {{1}}", Bindings: []campaign.VariableBinding{firstNameBinding()}}
	h.campaigns.put(c)

	msg, err := h.svc.TestSend(context.Background(), wsID, "c1", Actor{UserID: "user-9"}, phoneOf(1), 1)
	if err != nil || msg == nil {
		t.Fatalf("test send %v %v", msg, err)
	}
	req := h.carrier.requests[0]
	if req.RunKind != carriermessaging.RunTest || req.Marketing || req.Variant != 1 || req.Phone != shared.NormalizePhone(phoneOf(1)) ||
		req.Lead == nil || req.Lead.ID != "lead-a" || len(req.Vars) != 1 || req.Vars[0] != "Beatriz" || req.Content.Text != "Olá {{1}}" ||
		req.SenderID != "sender-1" || req.Channel != carriermessaging.ChannelRCS || req.Fallback.SMSText != "Oi Beatriz" || req.Fallback.SMSRouteID != "route-1" {
		t.Fatalf("request %+v", req)
	}
}

func TestTestSendRefusals(t *testing.T) {
	cases := []struct {
		name    string
		phone   string
		variant int
		err     error
	}{
		{"unknown variant", phoneOf(1), 3, ErrVariantIndex},
		{"negative variant", phoneOf(1), -1, ErrVariantIndex},
		{"invalid phone", "abc", 0, carriermessaging.ErrPhoneInvalid},
		{"no lead to fill the name", phoneOf(7), 0, campaign.ErrMissingVariable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.addLead("lead-a", 1)
			h.campaigns.put(rcsCampaign("c1"))
			if _, err := h.svc.TestSend(context.Background(), wsID, "c1", Actor{UserID: owner}, tc.phone, tc.variant); !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if len(h.carrier.requests) != 0 {
				t.Fatal("nothing should be sent")
			}
		})
	}
}

func TestTestSendLiteralNeedsNoLead(t *testing.T) {
	h := newHarness(t)
	c := smsCampaign("c1")
	c.Variants = []cc.Variant{{Text: "Cupom {{1}}", Bindings: []campaign.VariableBinding{{Source: campaign.BindLiteral, Value: "VOZ10"}}}}
	h.campaigns.put(c)
	if _, err := h.svc.TestSend(context.Background(), wsID, "c1", Actor{UserID: owner}, phoneOf(8), 0); err != nil {
		t.Fatal(err)
	}
	req := h.carrier.requests[0]
	if req.Lead != nil || req.Text != "Cupom {{1}}" || req.Vars[0] != "VOZ10" || req.Channel != carriermessaging.ChannelSMS || req.SenderID != "route-1" {
		t.Fatalf("request %+v", req)
	}
}

func TestEstimateRefusesTextTooLongWithThePrefix(t *testing.T) {
	h := newHarness(t)
	h.addLead("lead-a", 1)
	c := smsCampaign("c1")
	c.Variants = []cc.Variant{{Text: strings.Repeat("a", 8*153-3)}}
	h.campaigns.put(c)
	if _, err := h.svc.Estimate(context.Background(), wsID, "c1", Actor{UserID: owner}); !errors.Is(err, sms.ErrTextTooLong) {
		t.Fatalf("err = %v", err)
	}
	if code, ok := BlockerOf(sms.ErrTextTooLong); ok {
		t.Fatalf("a text too long is not a readiness blocker, got %q", code)
	}
}

func TestEstimateCarriesTheAudienceFingerprint(t *testing.T) {
	h := newHarness(t)
	h.addLead("lead-a", 1)
	c := rcsCampaign("c1")
	c.Audience = everyone(0)
	h.campaigns.put(c)
	got, err := h.svc.Estimate(context.Background(), wsID, "c1", Actor{UserID: owner})
	if err != nil || got.AudienceFingerprint != c.AudienceFingerprint() || got.AudienceFingerprint == "" {
		t.Fatalf("Estimate = %+v, %v", got, err)
	}
}
