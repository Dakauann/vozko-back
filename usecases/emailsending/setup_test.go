package emailsending

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	es "vozko/domain/emailsending"
)

func TestNewRefusesIncompleteDependencies(t *testing.T) {
	h := newHarness(t)
	cases := map[string]func(*Deps){
		"no provider":        func(d *Deps) { d.Provider = nil },
		"no codec":           func(d *Deps) { d.Codec = nil },
		"no alerts":          func(d *Deps) { d.Alerts = nil },
		"bad probation":      func(d *Deps) { d.Config.ProbationDomain = "nodot" },
		"same shared":        func(d *Deps) { d.Config.TrustedDomain = d.Config.ProbationDomain },
		"http public base":   func(d *Deps) { d.Config.PublicBaseURL = "http://api.test" },
		"empty public base":  func(d *Deps) { d.Config.PublicBaseURL = "" },
		"nested shared":      func(d *Deps) { d.Config.TrustedDomain = "x.envios.test" },
		"no workspace names": func(d *Deps) { d.Workspaces = nil },
		"no record lookup":   func(d *Deps) { d.Records = nil },
		"no reputation":      func(d *Deps) { d.Reputation = nil },
		"no hygiene":         func(d *Deps) { d.Hygiene = nil },
		"no test batches":    func(d *Deps) { d.TestBatches = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			d := h.deps()
			mutate(&d)
			if _, err := New(d); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestStatusBeforeProvisioning(t *testing.T) {
	h := newHarness(t)
	view, err := h.m.Setup.Status(context.Background(), wsID)
	if err != nil || view.Provisioned {
		t.Fatalf("view = %+v err = %v", view, err)
	}
}

func TestProvisionAnswersRightAwayWithTheSetupInProgress(t *testing.T) {
	h := newHarness(t)
	view, err := h.m.Setup.Provision(context.Background(), wsID, "")
	if err != nil {
		t.Fatal(err)
	}
	if !view.Provisioned || view.Account == nil || view.Account.Status != es.AccountActive || view.Account.TenantRef != "" {
		t.Fatalf("account = %+v", view.Account)
	}
	if len(view.Domains) != 1 || view.Domains[0].Domain.Status != es.DomainPending || view.Domains[0].Domain.Domain != "loja-da-joana.envios.test" {
		t.Fatalf("domains = %+v", view.Domains)
	}
	if view.Setup != (es.SetupProgress{State: es.SetupInProgress}) {
		t.Fatalf("setup = %+v", view.Setup)
	}
	if h.provider.tenantCalls != 0 || len(h.provider.identities) != 0 || len(h.dns.upserts) != 0 || len(h.routes.ensured) != 0 {
		t.Fatalf("the request touched the provider: tenants %d identities %v dns %v routes %v", h.provider.tenantCalls, h.provider.identities, h.dns.upserts, h.routes.ensured)
	}
	if kicks := h.wake.take(); len(kicks) != 1 || kicks[0] != wsID {
		t.Fatalf("kicks = %v, want the workspace setup started now", kicks)
	}
}

func TestTheSetupRunBuildsTheProbationSetup(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	view := h.provision(t, wsID)
	acct := view.Account
	if !view.Provisioned || acct.Slug != "loja-da-joana" || acct.TenantName != "ws-"+wsID || acct.Mode != es.ModeProbation ||
		acct.Status != es.AccountActive || acct.TenantRef != "arn:tenant/ws-"+wsID || !acct.ProbationStartedAt.Equal(testNow) {
		t.Fatalf("account = %+v", acct)
	}
	if len(view.Domains) != 1 || view.Setup.State != es.SetupDone {
		t.Fatalf("domains = %+v setup = %+v", view.Domains, view.Setup)
	}
	d := view.Domains[0]
	if d.Domain.Domain != "loja-da-joana.envios.test" || d.Domain.Kind != es.DomainSharedProbation || d.Domain.Status != es.DomainVerifying ||
		d.Domain.ReplyDomain != "loja-da-joana.envios.test" || d.Domain.MailFromDomain != "bounce.loja-da-joana.envios.test" || d.CapToday != es.ProbationDailyCap {
		t.Fatalf("domain = %+v", d)
	}
	if got := h.dns.upserts["envios.test"]; len(got) != 4 {
		t.Fatalf("dns upserts = %+v", h.dns.upserts)
	}
	if len(h.routes.ensured) != 1 || h.routes.ensured[0] != "loja-da-joana.envios.test" {
		t.Fatalf("inbound = %v", h.routes.ensured)
	}
	if view.Probation == nil || view.Probation.RequiredDays != 14 || view.Probation.Outcome != es.PromotionTooSoon {
		t.Fatalf("probation = %+v", view.Probation)
	}

	again, err := h.m.Setup.Provision(ctx, wsID, "")
	if err != nil || len(again.Domains) != 1 || len(h.accounts.rows) != 1 || again.Setup.State != es.SetupDone {
		t.Fatalf("provision must be idempotent: %+v %v", again, err)
	}
	if kicks := h.wake.take(); len(kicks) != 0 {
		t.Fatalf("a finished setup starts no run: %v", kicks)
	}
}

func TestTheSetupRunUsesTheAccountLaneAndTheTenantLock(t *testing.T) {
	h := newHarness(t)
	if _, err := h.m.Setup.Provision(context.Background(), wsID, ""); err != nil {
		t.Fatal(err)
	}
	release, err := h.locks.Acquire(context.Background(), tenantLockKey(wsID))
	if err != nil {
		t.Fatal(err)
	}
	waiting, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := h.m.Setup.Complete(waiting, wsID); !errors.Is(err, context.DeadlineExceeded) || h.provider.tenantCalls != 0 {
		t.Fatalf("a run while another holds the workspace lock = %v, tenant calls %d", err, h.provider.tenantCalls)
	}
	if _, err := release(); err != nil {
		t.Fatal(err)
	}
	if err := h.runSetup(); err != nil {
		t.Fatal(err)
	}
	for _, call := range []string{"ensure tenant", "ensure identity"} {
		if got := h.provider.lanesOf(call); len(got) != 1 || got[0] != es.LaneAccount {
			t.Errorf("%s lanes = %v, want account", call, got)
		}
	}
}

func TestAProviderFailureIsRecordedWithItsCodeAndRetried(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.dns.err = errors.New("dns provider down")
	if _, err := h.m.Setup.Provision(ctx, wsID, ""); err != nil {
		t.Fatal(err)
	}
	if err := h.runSetup(); !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("run err = %v", err)
	}
	view, err := h.m.Setup.Status(ctx, wsID)
	if err != nil {
		t.Fatal(err)
	}
	d := view.Domains[0].Domain
	if d.Status != es.DomainPending || d.SetupFailure != es.SetupFailureProvider || !strings.HasPrefix(d.FailureReason, "dns: ") {
		t.Fatalf("domain = %+v", d)
	}
	if view.Setup != (es.SetupProgress{State: es.SetupFailed, Failure: es.SetupFailureProvider}) {
		t.Fatalf("setup = %+v", view.Setup)
	}

	h.dns.err = nil
	retry, err := h.m.Setup.Provision(ctx, wsID, "")
	if err != nil || retry.Setup.State != es.SetupInProgress || retry.Domains[0].Domain.FailureReason != "" {
		t.Fatalf("a retry shows the setup in progress again: %+v %v", retry.Setup, err)
	}
	if err := h.runSetup(); err != nil {
		t.Fatal(err)
	}
	stored, _ := h.domains.Get(ctx, wsID, d.ID)
	if stored.Status != es.DomainVerifying || stored.FailureReason != "" || stored.SetupFailure != "" || stored.VerifyingSince == nil {
		t.Fatalf("after retry = %+v", stored)
	}
}

func TestTheVerificationSweepFinishesASetupWhoseRunWasLost(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.m.Setup.Provision(ctx, wsID, ""); err != nil {
		t.Fatal(err)
	}
	h.wake.take()
	if err := h.m.Verification.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	d, err := h.m.c().domainOfKind(ctx, wsID, es.DomainSharedProbation)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != es.DomainVerifying || len(h.routes.ensured) != 1 {
		t.Fatalf("domain = %+v routes = %v", d, h.routes.ensured)
	}
	if got := h.provider.lanesOf("ensure identity"); len(got) != 1 || got[0] != es.LaneBackground {
		t.Fatalf("sweep lanes = %v, want background", got)
	}
}

func TestTheFailureNoteIsSavedWhenTheRunIsCancelled(t *testing.T) {
	h := newHarness(t)
	if _, err := h.m.Setup.Provision(context.Background(), wsID, ""); err != nil {
		t.Fatal(err)
	}
	h.wake.take()
	ctx, cancel := context.WithCancel(context.Background())
	h.provider.onIdentity = cancel
	h.provider.identityErr = context.Canceled
	if err := h.m.Setup.Complete(ctx, wsID); !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("err = %v", err)
	}
	d, err := h.m.c().domainOfKind(context.Background(), wsID, es.DomainSharedProbation)
	if err != nil {
		t.Fatal(err)
	}
	if d.SetupFailure != es.SetupFailureProvider || !strings.HasPrefix(d.FailureReason, "identity: ") {
		t.Fatalf("the failure note was lost: %+v", d)
	}
}

func TestATenantFailureIsRecordedOnThePendingDomain(t *testing.T) {
	h := newHarness(t)
	h.provider.tenantErr = errors.New("ses down")
	if _, err := h.m.Setup.Provision(context.Background(), wsID, ""); err != nil {
		t.Fatal(err)
	}
	if err := h.runSetup(); !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("err = %v", err)
	}
	d, err := h.m.c().domainOfKind(context.Background(), wsID, es.DomainSharedProbation)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != es.DomainPending || d.SetupFailure != es.SetupFailureProvider || !strings.HasPrefix(d.FailureReason, "tenant: ") || len(h.provider.identities) != 0 {
		t.Fatalf("domain = %+v identities = %v", d, h.provider.identities)
	}
}

func TestAKickThatCannotBeQueuedIsShownAsAFailure(t *testing.T) {
	h := newHarness(t)
	h.wake.err = errors.New("broker down")
	view, err := h.m.Setup.Provision(context.Background(), wsID, "")
	if err != nil {
		t.Fatal(err)
	}
	if view.Setup != (es.SetupProgress{State: es.SetupFailed, Failure: es.SetupFailureProvider}) || !strings.HasPrefix(view.Domains[0].Domain.FailureReason, "queue: ") {
		t.Fatalf("setup = %+v domain = %+v", view.Setup, view.Domains[0].Domain)
	}
}

func TestConcurrentProvisionsShareOneDomain(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.m.Setup.Provision(ctx, wsID, ""); err != nil {
		t.Fatal(err)
	}
	h.domains.staleReads = 1
	view, err := h.m.Setup.Provision(ctx, wsID, "")
	if err != nil {
		t.Fatalf("a provision that lost the race must read the winner's row: %v", err)
	}
	if len(view.Domains) != 1 || len(h.domains.rows) != 1 {
		t.Fatalf("domains = %+v", view.Domains)
	}
}

func TestSlugCollisionGetsSuffix(t *testing.T) {
	h := newHarness(t)
	h.accounts.rows["x"] = es.SenderAccount{ID: "x", WorkspaceID: "other", Slug: "loja-da-joana", TenantName: "ws-other"}
	view := h.provision(t, wsID)
	if view.Account.Slug != "loja-da-joana-2" {
		t.Fatalf("slug = %q", view.Account.Slug)
	}
}

func TestVerifiedProbationDomainGetsADefaultSender(t *testing.T) {
	h := newHarness(t)
	d := h.provisionVerified(t, wsID)
	if d.Status != es.DomainVerified || d.VerifiedAt == nil {
		t.Fatalf("domain = %+v", d)
	}
	def, err := h.senders.GetDefault(context.Background(), wsID)
	if err != nil {
		t.Fatal(err)
	}
	if def.LocalPart != "contato" || def.DisplayName != wsName || def.DomainID != d.ID {
		t.Fatalf("default sender = %+v", def)
	}
	for _, key := range h.alerts.keys() {
		if strings.HasPrefix(key, "email_domain_verified") {
			t.Fatal("shared domains do not notify the workspace")
		}
	}
}

func TestAProvisionRetryRestartsEveryPendingDomainOfTheWorkspace(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.provision(t, wsID)
	h.dmarc.err = errors.New("servfail")
	own, err := h.m.Domains.Connect(ctx, wsID, "loja.com.br")
	if err != nil {
		t.Fatal(err)
	}
	_ = h.runSetup()
	h.dmarc.err = nil
	view, err := h.m.Setup.Provision(ctx, wsID, "")
	if err != nil {
		t.Fatal(err)
	}
	if view.Setup.State != es.SetupInProgress {
		t.Fatalf("setup = %+v", view.Setup)
	}
	if err := h.runSetup(); err != nil {
		t.Fatal(err)
	}
	if d, _ := h.domains.Get(ctx, wsID, own.ID); d.Status != es.DomainVerifying || d.FailureReason != "" {
		t.Fatalf("own domain after the retry = %+v", d)
	}
}
