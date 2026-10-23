package emailsending

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	es "vozko/domain/emailsending"
	"vozko/domain/notification"
)

const otherWS = "22222222-2222-2222-2222-222222222222"

func (h *harness) seedReplyDomains() {
	paused := testNow.Add(-time.Hour)
	for _, d := range []es.SendingDomain{
		{ID: "d-trusted", WorkspaceID: wsID, Domain: "joana.mail.test", Kind: es.DomainSharedTrusted, ReplyDomain: "joana.mail.test", ConnectMethod: es.ConnectAutomatic},
		{ID: "d-probation", WorkspaceID: wsID, Domain: "joana.envios.test", Kind: es.DomainSharedProbation, ReplyDomain: "joana.envios.test", Retired: true, ConnectMethod: es.ConnectAutomatic},
		{ID: "d-own", WorkspaceID: wsID, Domain: "loja.com.br", Kind: es.DomainOwn, ReplyDomain: "reply.loja.com.br", ConnectMethod: es.ConnectManual},
		{ID: "d-removed", WorkspaceID: wsID, Domain: "velha.com.br", Kind: es.DomainOwn, ReplyDomain: "reply.velha.com.br", Retired: true, ConnectMethod: es.ConnectManual},
		{ID: "d-other", WorkspaceID: otherWS, Domain: "outra.mail.test", Kind: es.DomainSharedTrusted, ReplyDomain: "outra.mail.test", ConnectMethod: es.ConnectAutomatic, RepliesPausedAt: &paused},
	} {
		h.domains.rows[d.ID] = d
	}
}

func (h *harness) pausedDomains() []string {
	var out []string
	for _, d := range h.domains.list(func(d es.SendingDomain) bool { return d.RepliesPaused() }) {
		out = append(out, d.ID)
	}
	return out
}

func (h *harness) draftsWithKey(key string) []notification.Draft {
	var out []notification.Draft
	for _, d := range h.alerts.drafts {
		if d.EpisodeKey == key {
			out = append(out, d)
		}
	}
	return out
}

func TestPauseRepliesCoversEveryReplyDomainOfTheWorkspace(t *testing.T) {
	h := newHarness(t)
	h.seedReplyDomains()
	ctx := context.Background()
	if err := h.m.Replies.Pause(ctx, wsID); err != nil {
		t.Fatal(err)
	}
	slices.Sort(h.routes.paused)
	if want := []string{"joana.envios.test", "joana.mail.test", "reply.loja.com.br"}; !slices.Equal(h.routes.paused, want) {
		t.Fatalf("paused routes = %v, want %v", h.routes.paused, want)
	}
	if got := h.pausedDomains(); !slices.Equal(got, []string{"d-other", "d-own", "d-probation", "d-trusted"}) {
		t.Fatalf("flagged = %v", got)
	}
	drafts := h.draftsWithKey("email_replies_paused:" + wsID)
	if len(drafts) != 1 {
		t.Fatalf("drafts = %+v", h.alerts.drafts)
	}
	d := drafts[0]
	if d.Severity != notification.SeverityCritical || d.Category != notification.CategoryBilling || d.ActionURL != WalletTopUpPath ||
		!d.Audience.Equal(notification.Managers()) || !strings.Contains(d.Body, "saldo") || strings.ContainsRune(d.Title+d.Body, 0x2013) || strings.ContainsRune(d.Title+d.Body, 0x2014) {
		t.Fatalf("draft = %+v", d)
	}
	if err := h.m.Replies.Pause(ctx, wsID); err != nil {
		t.Fatal(err)
	}
	if len(h.routes.paused) != 3 || len(h.draftsWithKey("email_replies_paused:"+wsID)) != 1 {
		t.Fatalf("a second pause repeated work: routes %v drafts %d", h.routes.paused, len(h.alerts.drafts))
	}
}

func TestPauseRepliesKeepsGoingPastAFailingDomain(t *testing.T) {
	h := newHarness(t)
	h.seedReplyDomains()
	h.routes.failPause["joana.mail.test"] = true
	err := h.m.Replies.Pause(context.Background(), wsID)
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if got := h.pausedDomains(); slices.Contains(got, "d-trusted") || !slices.Contains(got, "d-own") || !slices.Contains(got, "d-probation") {
		t.Fatalf("flagged = %v", got)
	}
	if len(h.draftsWithKey("email_replies_paused:"+wsID)) != 1 {
		t.Fatal("the workspace is told as soon as any reply domain is paused")
	}
}

func TestPauseRepliesLeavesTheFlagUnsetWhenItCannotBeStored(t *testing.T) {
	h := newHarness(t)
	h.seedReplyDomains()
	h.pauses.markErr = errors.New("db down")
	if err := h.m.Replies.Pause(context.Background(), wsID); err == nil {
		t.Fatal("expected error")
	}
	if len(h.draftsWithKey("email_replies_paused:"+wsID)) != 0 {
		t.Fatal("nothing was recorded as paused, so nothing is announced")
	}
}

func TestReplySweepResumesToppedUpWorkspaces(t *testing.T) {
	h := newHarness(t)
	h.seedReplyDomains()
	ctx := context.Background()
	h.pauses.balances[wsID] = -10
	h.pauses.balances[otherWS] = 0
	if err := h.m.ReplySweep.Execute(ctx); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(h.routes.resumed, []string{"outra.mail.test"}) || slices.Contains(h.pausedDomains(), "d-other") {
		t.Fatalf("resumed = %v flagged = %v", h.routes.resumed, h.pausedDomains())
	}
	if !slices.Contains(h.alerts.resolved, "email_replies_paused:"+otherWS) {
		t.Fatalf("resolved = %v", h.alerts.resolved)
	}
	if len(h.routes.paused) != 3 || !slices.Contains(h.pausedDomains(), "d-trusted") {
		t.Fatalf("a negative wallet with receiving domains is paused by the safety net: %v", h.routes.paused)
	}
	if len(h.routes.unblockedAt) != 1 || !h.routes.unblockedAt[0].Equal(testNow) {
		t.Fatalf("unblock = %v", h.routes.unblockedAt)
	}
}

func TestReplySweepKeepsAPauseItCouldNotLift(t *testing.T) {
	h := newHarness(t)
	h.seedReplyDomains()
	h.pauses.balances[otherWS] = 500
	h.routes.failResume["outra.mail.test"] = true
	if err := h.m.ReplySweep.Execute(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if !slices.Contains(h.pausedDomains(), "d-other") || slices.Contains(h.alerts.resolved, "email_replies_paused:"+otherWS) {
		t.Fatalf("flagged = %v resolved = %v", h.pausedDomains(), h.alerts.resolved)
	}
}

func TestReplySweepAdoptsLegacyRoutesOncePerProcess(t *testing.T) {
	h := newHarness(t)
	h.seedReplyDomains()
	ctx := context.Background()
	h.routes.adoptErr = errors.New("throttled")
	if err := h.m.ReplySweep.Execute(ctx); err == nil {
		t.Fatal("a failed adoption is reported")
	}
	h.routes.adoptErr = nil
	for i := 0; i < 2; i++ {
		if err := h.m.ReplySweep.Execute(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if len(h.routes.adopted) != 1 {
		t.Fatalf("adoptions = %d, want one after the failure", len(h.routes.adopted))
	}
	got := h.routes.adopted[0]
	slices.SortFunc(got, func(a, b es.ReplyRoute) int { return strings.Compare(a.ReplyDomain, b.ReplyDomain) })
	want := []es.ReplyRoute{{ReplyDomain: "joana.envios.test"}, {ReplyDomain: "joana.mail.test"}, {ReplyDomain: "outra.mail.test", Paused: true}, {ReplyDomain: "reply.loja.com.br"}}
	if !slices.Equal(got, want) {
		t.Fatalf("adopted = %+v, want %+v", got, want)
	}
}

func TestReplySweepReportsAnUnreadableHoldList(t *testing.T) {
	h := newHarness(t)
	h.pauses.listErr = errors.New("db down")
	if err := h.m.ReplySweep.Execute(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if len(h.routes.unblockedAt) != 1 {
		t.Fatal("expired sender blocks are lifted even when the hold list fails")
	}
}

func TestRecordSenderBlocksAFlood(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	ip := netip.MustParseAddr("203.0.113.7")
	for i := 0; i < es.InboundFloodThreshold-1; i++ {
		h.m.Replies.RecordSender(ctx, ip)
	}
	if len(h.routes.blocks) != 0 {
		t.Fatal("below the threshold nothing is blocked")
	}
	h.routes.blockNew = true
	h.m.Replies.RecordSender(ctx, ip)
	if len(h.routes.blocks) != 1 || h.routes.blocks[0].ip != ip || !h.routes.blocks[0].until.Equal(testNow.Add(es.InboundBlockDuration)) {
		t.Fatalf("blocks = %+v", h.routes.blocks)
	}
	if len(h.ops.alerts) != 1 || !strings.Contains(h.ops.alerts[0], "203.0.113.7") {
		t.Fatalf("ops = %v", h.ops.alerts)
	}
	h.routes.blockNew = false
	h.m.Replies.RecordSender(ctx, ip)
	if len(h.routes.blocks) != 2 || len(h.ops.alerts) != 1 {
		t.Fatalf("an existing block raises no second alert: %v", h.ops.alerts)
	}
	if _, ok := h.traffic.counts["email:inbound:sender:203.0.113.7"]; !ok {
		t.Fatalf("counter keys = %v", h.traffic.counts)
	}
}

func TestRecordSenderFailuresNeverDropMailButAreRaised(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.traffic.err = errors.New("redis down")
	h.m.Replies.RecordSender(ctx, netip.MustParseAddr("203.0.113.7"))
	if len(h.ops.alerts) != 1 || len(h.routes.blocks) != 0 {
		t.Fatalf("ops = %v blocks = %v", h.ops.alerts, h.routes.blocks)
	}
	h.traffic.err = nil
	h.traffic.counts["email:inbound:sender:203.0.113.8"] = es.InboundFloodThreshold
	h.routes.blockErr = errors.New("ses down")
	h.m.Replies.RecordSender(ctx, netip.MustParseAddr("203.0.113.8"))
	if len(h.ops.alerts) != 2 {
		t.Fatalf("ops = %v", h.ops.alerts)
	}
	h.m.Replies.RecordSender(ctx, netip.Addr{})
	if len(h.traffic.counts) != 1 {
		t.Fatalf("an unknown sender is never counted: %v", h.traffic.counts)
	}
}

func TestReplyRouteCapacityIsCodedAndRaised(t *testing.T) {
	h := newHarness(t)
	h.routes.ensureErr = es.ErrInboundCapacityReached
	if _, err := h.m.Setup.Provision(context.Background(), wsID, ""); err != nil {
		t.Fatal(err)
	}
	if err := h.runSetup(); !errors.Is(err, ErrReplyCapacityReached) {
		t.Fatalf("err = %v", err)
	}
	view, err := h.m.Setup.Status(context.Background(), wsID)
	if err != nil || view.Setup != (es.SetupProgress{State: es.SetupFailed, Failure: es.SetupFailureReplyCapacity}) {
		t.Fatalf("setup = %+v %v", view.Setup, err)
	}
	if len(h.ops.alerts) != 1 || !strings.Contains(h.ops.alerts[0], "receipt") {
		t.Fatalf("ops = %v", h.ops.alerts)
	}
}

func TestStatusSummarizesPausedReplies(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	d := h.provisionVerified(t, wsID)
	view, err := h.m.Setup.Status(ctx, wsID)
	if err != nil || view.RepliesPaused {
		t.Fatalf("view = %+v, %v", view.RepliesPaused, err)
	}
	if err := h.m.Replies.Pause(ctx, wsID); err != nil {
		t.Fatal(err)
	}
	view, err = h.m.Setup.Status(ctx, wsID)
	if err != nil || !view.RepliesPaused {
		t.Fatalf("view = %+v, %v", view.RepliesPaused, err)
	}
	for _, dv := range view.Domains {
		if dv.Domain.ID == d.ID && !dv.Domain.RepliesPaused() {
			t.Fatalf("domain = %+v", dv.Domain)
		}
	}
}
