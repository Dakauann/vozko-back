package emailsending

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	es "vozko/domain/emailsending"
)

func TestPromotionSweepMovesToTheTrustedDomainOnceVerified(t *testing.T) {
	h, probation := readyHarness(t)
	ctx := context.Background()
	extra, err := h.m.Senders.Create(ctx, wsID, CreateSenderInput{DomainID: probation.ID, LocalPart: "vendas", DisplayName: "Vendas"})
	if err != nil {
		t.Fatal(err)
	}
	now := testNow.Add(15 * 24 * time.Hour)
	h.now = now
	if err := h.m.Promotion.Sweep(ctx, now); err != nil {
		t.Fatal(err)
	}
	trusted, err := h.m.c().domainOfKind(ctx, wsID, es.DomainSharedTrusted)
	if err != nil {
		t.Fatal(err)
	}
	if trusted.Domain != "loja-da-joana.mail.test" || trusted.Status != es.DomainVerifying || len(h.dns.upserts["mail.test"]) != 4 {
		t.Fatalf("trusted = %+v upserts = %v", trusted, h.dns.upserts["mail.test"])
	}
	acct, _ := h.accounts.GetByWorkspace(ctx, wsID)
	if acct.Mode != es.ModeProbation {
		t.Fatal("promotion completes only when the trusted domain verifies")
	}

	h.provider.states[trusted.Domain] = es.IdentityState{DKIMVerified: true, MailFromVerified: true}
	if err := h.m.Verification.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	acct, _ = h.accounts.GetByWorkspace(ctx, wsID)
	if acct.Mode != es.ModeTrusted || acct.PromotedAt == nil || !acct.PromotedAt.Equal(now) {
		t.Fatalf("account = %+v", acct)
	}
	old, _ := h.domains.Get(ctx, wsID, probation.ID)
	if !old.Retired {
		t.Fatal("the probation domain is retired for sending")
	}
	for _, removed := range h.routes.removed {
		if removed == probation.ReplyDomain {
			t.Fatal("old reply addresses keep working")
		}
	}
	moved, _ := h.senders.Get(ctx, wsID, extra.Sender.ID)
	if moved.DomainID != trusted.ID {
		t.Fatalf("sender = %+v", moved)
	}
	senders, _ := h.senders.ListByWorkspace(ctx, wsID)
	if len(senders) != 2 {
		t.Fatalf("senders = %+v", senders)
	}
	if !strings.Contains(strings.Join(h.alerts.keys(), ","), "email_promoted:"+acct.ID) {
		t.Fatalf("drafts = %v", h.alerts.keys())
	}
	msg, err := h.m.Gateway.Send(ctx, marketingRequest(), compose("Oi"))
	if err != nil || msg.DomainID != trusted.ID {
		t.Fatalf("send after promotion = %+v %v", msg, err)
	}
}

func TestPromotionSweepRespectsHealth(t *testing.T) {
	cases := map[string]func(*harness){
		"open high finding": func(h *harness) {
			h.provider.health.Findings = []es.FindingEvent{{Impact: es.FindingHigh, Open: true}}
		},
		"tenant disabled":   func(h *harness) { h.provider.health.Sending = es.TenantDisabled },
		"failed test batch": func(h *harness) { h.testBatches.failed = 1 },
	}
	for name, prepare := range cases {
		t.Run(name, func(t *testing.T) {
			h, _ := readyHarness(t)
			prepare(h)
			now := testNow.Add(15 * 24 * time.Hour)
			if err := h.m.Promotion.Sweep(context.Background(), now); err != nil {
				t.Fatal(err)
			}
			if _, err := h.m.c().domainOfKind(context.Background(), wsID, es.DomainSharedTrusted); !errors.Is(err, es.ErrDomainNotFound) {
				t.Fatal("no promotion expected")
			}
		})
	}
	t.Run("health unavailable", func(t *testing.T) {
		h, _ := readyHarness(t)
		h.provider.healthErr = errors.New("ses down")
		if err := h.m.Promotion.Sweep(context.Background(), testNow.Add(15*24*time.Hour)); err == nil {
			t.Fatal("unknown health must not promote and must report")
		}
	})
}

func TestAdminPauseReinstatePromote(t *testing.T) {
	h, _ := readyHarness(t)
	ctx := context.Background()
	acct, err := h.m.Admin.Pause(ctx, wsID, "")
	if err != nil || acct.Status != es.AccountPausedByPlatform || acct.StatusReason == "" || h.provider.sendingCalls[0] {
		t.Fatalf("pause = %+v %v %v", acct, err, h.provider.sendingCalls)
	}
	h.provider.health.Sending = es.TenantDisabled
	acct, err = h.m.Admin.Reinstate(ctx, wsID)
	if err != nil || acct.Status != es.AccountPausedByProvider {
		t.Fatalf("reinstate while the provider keeps it disabled = %+v %v", acct, err)
	}
	h.provider.health.Sending = es.TenantReinstated
	acct, err = h.m.Admin.Reinstate(ctx, wsID)
	if err != nil || acct.Status != es.AccountActive {
		t.Fatalf("reinstate = %+v %v", acct, err)
	}
	views, total, err := h.m.Admin.List(ctx, 0, 0)
	if err != nil || total != 1 || views[0].Health == nil {
		t.Fatalf("list = %+v %d %v", views, total, err)
	}
	if _, err := h.m.Admin.Promote(ctx, wsID); err != nil {
		t.Fatal(err)
	}
	trusted, err := h.m.c().domainOfKind(ctx, wsID, es.DomainSharedTrusted)
	if err != nil || trusted.SetupProgress().State != es.SetupInProgress {
		t.Fatalf("forced promotion answers with the trusted domain being set up: %+v %v", trusted, err)
	}
	if err := h.runSetup(); err != nil {
		t.Fatal(err)
	}
	if trusted, _ = h.domains.Get(ctx, wsID, trusted.ID); trusted.Status != es.DomainVerifying {
		t.Fatalf("trusted after the run = %+v", trusted)
	}
	acct.Mode = es.ModeTrusted
	_ = h.accounts.Update(ctx, acct)
	if _, err := h.m.Admin.Promote(ctx, wsID); !errors.Is(err, ErrNotInProbation) {
		t.Fatalf("err = %v", err)
	}
}

func TestDeliverabilitySweep(t *testing.T) {
	h, _ := readyHarness(t)
	ctx := context.Background()
	res := h.connect(t, wsID, "loja.com.br")
	h.provider.states["loja.com.br"] = es.IdentityState{DKIMVerified: true, MailFromVerified: true}
	if _, err := h.m.Domains.Recheck(ctx, wsID, res.ID); err != nil {
		t.Fatal(err)
	}
	h.monitor.stats["envios.test"] = es.DomainStats{Available: true, SpamRatePerMil: 4, Day: "2026-10-20"}
	h.monitor.errs["mail.test"] = errors.New("forbidden")
	h.monitor.stats["loja.com.br"] = es.DomainStats{Available: true, SpamRatePerMil: 5, Day: "2026-10-20"}
	if err := h.m.Deliverability.Sweep(ctx, testNow); err == nil {
		t.Fatal("a failed check must be reported")
	}
	if len(h.ops.alerts) != 1 || !strings.Contains(h.ops.alerts[0], "envios.test") {
		t.Fatalf("ops alerts = %v", h.ops.alerts)
	}
	keys := h.alerts.keys()
	if keys[len(keys)-1] != spamRateKey(res.ID) {
		t.Fatalf("drafts = %v", keys)
	}
	h.monitor.stats["loja.com.br"] = es.DomainStats{Available: true, SpamRatePerMil: 1}
	delete(h.monitor.errs, "mail.test")
	if err := h.m.Deliverability.Sweep(ctx, testNow); err != nil {
		t.Fatal(err)
	}
	if h.alerts.resolved[len(h.alerts.resolved)-1] != spamRateKey(res.ID) {
		t.Fatalf("resolved = %v", h.alerts.resolved)
	}
}

func TestSendersRules(t *testing.T) {
	h, d := readyHarness(t)
	ctx := context.Background()
	if _, err := h.m.Senders.Create(ctx, wsID, CreateSenderInput{DomainID: d.ID, LocalPart: "postmaster", DisplayName: "x"}); !errors.Is(err, es.ErrInvalidLocalPart) {
		t.Fatalf("err = %v", err)
	}
	if _, err := h.m.Senders.Create(ctx, wsID, CreateSenderInput{DomainID: d.ID, LocalPart: "vendas", DisplayName: "a<b"}); !errors.Is(err, es.ErrInvalidDisplayName) {
		t.Fatalf("err = %v", err)
	}
	if _, err := h.m.Senders.Create(ctx, wsID, CreateSenderInput{DomainID: d.ID, LocalPart: "vendas", DisplayName: "V", ReplyForwardTo: "nope"}); !errors.Is(err, es.ErrInvalidForward) {
		t.Fatalf("err = %v", err)
	}
	if _, err := h.m.Senders.Create(ctx, wsID, CreateSenderInput{DomainID: "missing", LocalPart: "vendas", DisplayName: "V"}); !errors.Is(err, es.ErrDomainNotFound) {
		t.Fatalf("err = %v", err)
	}
	view, err := h.m.Senders.Create(ctx, wsID, CreateSenderInput{DomainID: d.ID, LocalPart: "Vendas", DisplayName: "Vendas", IsDefault: true})
	if err != nil || view.Address != "vendas@"+d.Domain || !view.Sender.IsDefault {
		t.Fatalf("view = %+v err = %v", view, err)
	}
	if _, err := h.m.Senders.Create(ctx, wsID, CreateSenderInput{DomainID: d.ID, LocalPart: "vendas", DisplayName: "Outro"}); !errors.Is(err, es.ErrSenderExists) {
		t.Fatalf("err = %v", err)
	}
	no := false
	if _, err := h.m.Senders.Update(ctx, wsID, view.Sender.ID, UpdateSenderInput{IsDefault: &no}); !errors.Is(err, ErrDefaultSenderRequired) {
		t.Fatalf("err = %v", err)
	}
	if err := h.m.Senders.Delete(ctx, wsID, view.Sender.ID); !errors.Is(err, ErrDefaultSenderRequired) {
		t.Fatalf("err = %v", err)
	}
	name := "Equipe de vendas"
	updated, err := h.m.Senders.Update(ctx, wsID, view.Sender.ID, UpdateSenderInput{DisplayName: &name})
	if err != nil || updated.Sender.DisplayName != name || !updated.Sender.IsDefault {
		t.Fatalf("updated = %+v err = %v", updated, err)
	}
	list, err := h.m.Senders.List(ctx, wsID)
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %+v err = %v", list, err)
	}
}

func TestSuppressionsRules(t *testing.T) {
	h, _ := readyHarness(t)
	ctx := context.Background()
	if _, err := h.m.Suppressions.Add(ctx, wsID, "not an address"); !errors.Is(err, ErrInvalidAddress) {
		t.Fatalf("err = %v", err)
	}
	if _, err := h.m.Suppressions.Add(ctx, wsID, "Pessoa@Example.com"); err != nil {
		t.Fatal(err)
	}
	if h.provider.suppressed[0] != "pessoa@example.com" {
		t.Fatalf("provider = %v", h.provider.suppressed)
	}
	_ = h.suppressions.Suppress(ctx, es.Suppression{WorkspaceID: wsID, Address: "queixa@example.com", Reason: es.SuppressComplaint})
	if err := h.m.Suppressions.Remove(ctx, wsID, "queixa@example.com"); !errors.Is(err, ErrSuppressionLocked) {
		t.Fatalf("err = %v", err)
	}
	if err := h.m.Suppressions.Remove(ctx, wsID, "ninguem@example.com"); !errors.Is(err, ErrSuppressionNotFound) {
		t.Fatalf("err = %v", err)
	}
	if err := h.m.Suppressions.Remove(ctx, wsID, "pessoa@example.com"); err != nil {
		t.Fatal(err)
	}
	if list, total, _ := h.m.Suppressions.List(ctx, wsID, "", 0, 50); total != 1 || len(list) != 1 {
		t.Fatalf("list = %+v", list)
	}
}

func TestDisplayNameFrom(t *testing.T) {
	cases := map[string]string{
		"Loja da Joana":         "Loja da Joana",
		"  \"Aspas\" <x>  ":     "Aspas x",
		"":                      "fallback",
		strings.Repeat("a", 90): strings.Repeat("a", 80),
	}
	for in, want := range cases {
		if got := displayNameFrom(in, "fallback"); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestSuppressionsKeepHardBouncesForeverAndFreeSoftBounces(t *testing.T) {
	h, _ := readyHarness(t)
	ctx := context.Background()
	_ = h.suppressions.Suppress(ctx, es.Suppression{WorkspaceID: wsID, Address: "sumiu@example.com", Reason: es.SuppressHardBounce})
	_ = h.suppressions.Suppress(ctx, es.Suppression{WorkspaceID: wsID, Address: "cheia@example.com", Reason: es.SuppressSoftBounce})
	if err := h.m.Suppressions.Remove(ctx, wsID, "sumiu@example.com"); !errors.Is(err, ErrSuppressionLocked) {
		t.Fatalf("err = %v, a hard bounce is never retried", err)
	}
	if err := h.m.Suppressions.Remove(ctx, wsID, "cheia@example.com"); err != nil {
		t.Fatalf("a soft bounce streak can be lifted, got %v", err)
	}
}
