package carriermessaging_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/carriermessaging"
	"vozko/domain/conversation"
	"vozko/domain/rcs"
	"vozko/domain/sms"
)

func smsMarketing(text string) DeliverRequest {
	return DeliverRequest{
		WorkspaceID: "ws-1", Channel: carriermessaging.ChannelSMS, SenderID: "route-1", Phone: "+55 11 99999-0000",
		RunKind: carriermessaging.RunCampaign, Marketing: true, RunID: "camp-1", Text: text,
		SentBy: conversation.SentByCampaign("camp-1"),
	}
}

func TestSMSDeliverySendsTheBrandedTextAndChargesPerSegment(t *testing.T) {
	h := newHarness(t)
	req := smsMarketing("Oi {{1}}, " + strings.Repeat("a", 150))
	req.Vars = []string{"Ana"}

	msg, err := h.svc.Deliver(context.Background(), req)
	if err != nil {
		t.Fatalf("Deliver = %v", err)
	}
	if msg.Channel != carriermessaging.ChannelSMS || msg.Step != carriermessaging.StepSMS || msg.Segments != 2 || msg.Status != carriermessaging.StatusAccepted {
		t.Fatalf("message = %+v", msg)
	}
	if len(h.provider.sms) != 1 || h.provider.sms[0].From != "29000" || !strings.HasPrefix(h.provider.sms[0].Text, "Loja: Oi Ana, ") {
		t.Fatalf("sms = %+v", h.provider.sms)
	}
	if len(h.provider.sends) != 0 {
		t.Fatal("an SMS never goes through the RCS provider")
	}
	if len(h.ledger.debits) != 1 || h.ledger.debits[0].Amount != 70_000 || h.ledger.debits[0].ServiceType != "sms" {
		t.Fatalf("debits = %+v", h.ledger.debits)
	}
	rec := h.transcript.records[0]
	if rec.EntryType != carriermessaging.ChannelSMS.EntryType() || rec.EntryID != msg.ConversationID || !strings.HasPrefix(msg.ConversationID, "smsconv") {
		t.Fatalf("transcript = %+v", rec)
	}
}

func TestSMSFromADedicatedRoute(t *testing.T) {
	h := newHarness(t)
	h.routes.rows["route-1"].Kind, h.routes.rows["route-1"].Number = sms.RouteDedicated, "29123"
	if _, err := h.svc.Deliver(context.Background(), smsMarketing("Oi")); err != nil {
		t.Fatalf("Deliver = %v", err)
	}
	if h.provider.sms[0].From != "29123" {
		t.Fatalf("from = %q", h.provider.sms[0].From)
	}
}

func TestSMSRefusals(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*harness, *DeliverRequest)
		want  error
	}{
		{"paused route", func(h *harness, _ *DeliverRequest) { h.routes.rows["route-1"].Status = sms.RoutePaused }, sms.ErrRouteCannotSend},
		{"route of another workspace", func(_ *harness, r *DeliverRequest) { r.WorkspaceID = "ws-2" }, sms.ErrRouteNotFound},
		{"empty text", func(_ *harness, r *DeliverRequest) { r.Text = "" }, sms.ErrTextEmpty},
		{"too long", func(_ *harness, r *DeliverRequest) { r.Text = strings.Repeat("ã", 600) }, sms.ErrTextTooLong},
		{"shared frequency cap counts RCS too", func(h *harness, _ *DeliverRequest) {
			for _, id := range []string{"a", "b"} {
				h.messages.rows[id] = &carriermessaging.Message{ID: id, WorkspaceID: "ws-1", Phone: "5511999990000", Channel: carriermessaging.ChannelRCS, Marketing: true, Status: carriermessaging.StatusDelivered, CreatedAt: t0.Add(-time.Hour)}
			}
		}, carriermessaging.ErrFrequencyCap},
		{"suppressed by an RCS opt-out", func(h *harness, _ *DeliverRequest) {
			h.suppressions.rows["ws-1|5511999990000"] = carriermessaging.Suppression{}
		}, carriermessaging.ErrSuppressed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			req := smsMarketing("Oi")
			tc.setup(h, &req)
			if _, err := h.svc.Deliver(context.Background(), req); !errors.Is(err, tc.want) {
				t.Fatalf("Deliver = %v, want %v", err, tc.want)
			}
			if len(h.provider.sms) != 0 || len(h.ledger.debits) != 0 {
				t.Fatal("a refused SMS must not reach the provider or the ledger")
			}
		})
	}
}

func smsReply(id, to, text string, at time.Time) carriermessaging.Inbound {
	return carriermessaging.Inbound{ProviderMessageID: id, Channel: carriermessaging.ChannelSMS, Phone: "5511999990000", To: to, Text: text, ReceivedAt: at}
}

func TestSMSReplyOnTheSharedNumberFindsTheWorkspaceThatTexted(t *testing.T) {
	h := newHarness(t)
	msg, _ := h.svc.Deliver(context.Background(), smsMarketing("Oi"))
	_ = h.svc.ApplyStatus(context.Background(), carriermessaging.StatusReport{Reference: msg.Reference, Status: carriermessaging.StatusDelivered, ReceivedAt: t0})
	h.now = t0.Add(time.Hour)

	if err := h.svc.HandleInbound(context.Background(), smsReply("mo-1", "29000", "Quero", h.now)); err != nil {
		t.Fatalf("HandleInbound = %v", err)
	}
	last := h.transcript.records[len(h.transcript.records)-1]
	if last.EntryID != msg.ConversationID || last.Text != "Quero" || last.EntryType != carriermessaging.ChannelSMS.EntryType() {
		t.Fatalf("transcript = %+v", last)
	}
	if !hasDebit(h.ledger, carriermessaging.ChargeReference(carriermessaging.ChargeSMSInbound, "mo-1")) {
		t.Fatalf("debits = %+v", h.ledger.debits)
	}
	if len(h.inbox.dispatched) != 1 || h.inbox.dispatched[0].Channel != carriermessaging.ChannelSMS {
		t.Fatalf("dispatched = %+v", h.inbox.dispatched)
	}
}

func TestSMSReplyOnADedicatedNumber(t *testing.T) {
	h := newHarness(t)
	h.routes.rows["route-1"].Kind, h.routes.rows["route-1"].Number = sms.RouteDedicated, "29123"
	if err := h.svc.HandleInbound(context.Background(), smsReply("mo-1", "29123", "Oi", t0)); err != nil {
		t.Fatalf("HandleInbound = %v", err)
	}
	if len(h.inbox.dispatched) != 1 || h.inbox.dispatched[0].WorkspaceID != "ws-1" {
		t.Fatalf("dispatched = %+v", h.inbox.dispatched)
	}
}

func TestSMSReplyThatMatchesNoWorkspaceIsRefused(t *testing.T) {
	h := newHarness(t)
	for _, to := range []string{"29000", "40404"} {
		if err := h.svc.HandleInbound(context.Background(), smsReply("mo-"+to, to, "Oi", t0)); !errors.Is(err, sms.ErrInboundUnroutable) {
			t.Fatalf("HandleInbound(%s) = %v", to, err)
		}
	}
	if len(h.transcript.records) != 0 || len(h.ledger.debits) != 0 {
		t.Fatal("an unroutable reply touches nothing")
	}
}

func TestSMSOptOutSuppressesBothChannels(t *testing.T) {
	h := newHarness(t)
	h.routes.rows["route-1"].Kind, h.routes.rows["route-1"].Number = sms.RouteDedicated, "29123"
	_ = h.svc.HandleInbound(context.Background(), smsReply("mo-1", "29123", "PARAR", t0))
	if _, err := h.svc.Deliver(context.Background(), marketing(rcs.Content{Text: "Oi"})); !errors.Is(err, carriermessaging.ErrSuppressed) {
		t.Fatalf("RCS after an SMS opt-out = %v", err)
	}
}

func TestRoutes(t *testing.T) {
	h := newHarness(t)
	r, err := h.svc.CreateRoute(context.Background(), "ws-1", " Loja do Joao ")
	if err != nil || r.Kind != sms.RouteShared || r.Status != sms.RouteActive || r.BrandPrefix != "Loja do Joao" {
		t.Fatalf("CreateRoute = %+v, %v", r, err)
	}
	if _, err := h.svc.CreateRoute(context.Background(), "ws-1", "Promoção"); !errors.Is(err, sms.ErrRouteInvalid) {
		t.Fatalf("CreateRoute accents = %v", err)
	}
	if r, err = h.svc.UpdateRoute(context.Background(), "ws-1", r.ID, "Joao"); err != nil || r.BrandPrefix != "Joao" {
		t.Fatalf("UpdateRoute = %+v, %v", r, err)
	}
	if r, err = h.svc.PauseRoute(context.Background(), "ws-1", r.ID); err != nil || r.CanSend() {
		t.Fatalf("PauseRoute = %+v, %v", r, err)
	}
	if r, err = h.svc.ResumeRoute(context.Background(), "ws-1", r.ID); err != nil || !r.CanSend() {
		t.Fatalf("ResumeRoute = %+v, %v", r, err)
	}
	if r, err = h.svc.AdminAssignNumber(context.Background(), r.ID, "29124"); err != nil || r.Kind != sms.RouteDedicated || r.Number != "29124" {
		t.Fatalf("AdminAssignNumber = %+v, %v", r, err)
	}
	if _, err = h.svc.AdminAssignNumber(context.Background(), "route-1", "29124"); !errors.Is(err, ErrNumberTaken) {
		t.Fatalf("a number serves one route: %v", err)
	}
	if r, err = h.svc.AdminReleaseNumber(context.Background(), r.ID); err != nil || r.Kind != sms.RouteShared || r.Number != "" {
		t.Fatalf("AdminReleaseNumber = %+v, %v", r, err)
	}
}
