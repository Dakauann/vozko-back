package emailsending

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	es "vozko/domain/emailsending"
)

const otherWorkspace = "22222222-2222-2222-2222-222222222222"

func TestActivationUsesTheChosenSubdomain(t *testing.T) {
	h := newHarness(t)
	view, err := h.m.Setup.Provision(context.Background(), wsID, "cartaodetodososasco")
	if err != nil {
		t.Fatal(err)
	}
	if view.Account.Slug != "cartaodetodososasco" || view.Domains[0].Domain.Domain != "cartaodetodososasco.envios.test" {
		t.Fatalf("account %+v domains %+v", view.Account, view.Domains)
	}
	if view.Subdomain != (SubdomainView{Slug: "cartaodetodososasco", Address: "contato@cartaodetodososasco.envios.test"}) {
		t.Fatalf("subdomain = %+v", view.Subdomain)
	}
}

func TestActivationRefusesASubdomainItCannotHave(t *testing.T) {
	cases := []struct {
		name  string
		slug  string
		setup func(h *harness)
		want  error
	}{
		{name: "not canonical", slug: "Cartao Sasco", want: es.ErrInvalidSlug},
		{name: "reserved", slug: "postmaster", want: es.ErrSlugReserved},
		{name: "another account has it", slug: "outra-loja", setup: func(h *harness) {
			if _, err := h.m.Setup.Provision(context.Background(), otherWorkspace, "outra-loja"); err != nil {
				t.Fatal(err)
			}
		}, want: es.ErrSlugTaken},
		{name: "another workspace still holds its old domain", slug: "antiga", setup: func(h *harness) {
			if err := h.domains.Create(context.Background(), &es.SendingDomain{WorkspaceID: otherWorkspace, Domain: "antiga.mail.test", Kind: es.DomainSharedTrusted, Status: es.DomainVerified, ConnectMethod: es.ConnectAutomatic, ReplyDomain: "antiga.mail.test", Retired: true, ReleaseState: es.ReleaseDone}); err != nil {
				t.Fatal(err)
			}
		}, want: es.ErrSlugTaken},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			if tc.setup != nil {
				tc.setup(h)
			}
			if _, err := h.m.Setup.Provision(context.Background(), wsID, tc.slug); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if _, err := h.accounts.GetByWorkspace(context.Background(), wsID); !errors.Is(err, es.ErrAccountNotFound) {
				t.Fatalf("a refused activation created the account: %v", err)
			}
		})
	}
}

func TestCheckingASubdomainAnswersWithTheServerVerdictAndTheAddress(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.m.Setup.Provision(ctx, otherWorkspace, "outra-loja"); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		slug    string
		refusal error
		address string
	}{
		{"cartaodetodososasco", nil, "contato@cartaodetodososasco.envios.test"},
		{"outra-loja", es.ErrSlugTaken, "contato@outra-loja.envios.test"},
		{"mail", es.ErrSlugReserved, ""},
		{"Loja", es.ErrInvalidSlug, ""},
	}
	for _, tc := range cases {
		got, err := h.m.Setup.CheckSubdomain(ctx, wsID, tc.slug)
		if err != nil {
			t.Fatal(err)
		}
		if !errors.Is(got.Refusal, tc.refusal) || (tc.refusal == nil && got.Refusal != nil) || got.Address != tc.address || got.Slug != tc.slug {
			t.Errorf("%s = %+v, want refusal %v address %q", tc.slug, got, tc.refusal, tc.address)
		}
	}

	h.provision(t, wsID)
	acct, _ := h.accounts.GetByWorkspace(ctx, wsID)
	acct.Mode = es.ModeTrusted
	_ = h.accounts.Update(ctx, acct)
	own, err := h.m.Setup.CheckSubdomain(ctx, wsID, "loja-da-joana")
	if err != nil || own.Refusal != nil || own.Address != "contato@loja-da-joana.mail.test" {
		t.Fatalf("the workspace's own name on its current tier = %+v %v", own, err)
	}
}

func TestStatusSuggestsAFreeSubdomainBeforeActivation(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	view, err := h.m.Setup.Status(ctx, wsID)
	if err != nil || view.Subdomain != (SubdomainView{Slug: "loja-da-joana", Address: "contato@loja-da-joana.envios.test"}) {
		t.Fatalf("suggestion = %+v %v", view.Subdomain, err)
	}
	if _, err := h.m.Setup.Provision(ctx, otherWorkspace, "loja-da-joana"); err != nil {
		t.Fatal(err)
	}
	view, err = h.m.Setup.Status(ctx, wsID)
	if err != nil || view.Subdomain.Slug != "loja-da-joana-2" {
		t.Fatalf("suggestion with the name taken = %+v %v", view.Subdomain, err)
	}
}

func TestChangingTheSubdomainBeforeTheFirstSendMovesEverythingToTheNewName(t *testing.T) {
	h, old := readyHarness(t)
	ctx := context.Background()
	extra, err := h.m.Senders.Create(ctx, wsID, CreateSenderInput{DomainID: old.ID, LocalPart: "vendas", DisplayName: "Vendas"})
	if err != nil {
		t.Fatal(err)
	}
	view, err := h.m.Setup.ChangeSubdomain(ctx, wsID, "cartaodetodososasco")
	if err != nil {
		t.Fatal(err)
	}
	if view.Account.Slug != "cartaodetodososasco" || view.Setup.State != es.SetupInProgress {
		t.Fatalf("account = %+v setup = %+v", view.Account, view.Setup)
	}
	fresh, err := h.m.c().domainOfKind(ctx, wsID, es.DomainSharedProbation)
	if err != nil || fresh.Domain != "cartaodetodososasco.envios.test" || fresh.ID == old.ID {
		t.Fatalf("new domain = %+v %v", fresh, err)
	}
	stale, _ := h.domains.Get(ctx, wsID, old.ID)
	if !stale.Retired || !stale.AwaitsRelease() || stale.AwaitsSetup() {
		t.Fatalf("old domain = %+v", stale)
	}
	moved, _ := h.senders.Get(ctx, wsID, extra.Sender.ID)
	if moved.DomainID != fresh.ID {
		t.Fatalf("sender = %+v", moved)
	}
	if kicks := h.wake.take(); !slices.Equal(kicks, []string{wsID}) {
		t.Fatalf("kicks = %v", kicks)
	}

	if err := h.m.Setup.Complete(ctx, wsID); err != nil {
		t.Fatal(err)
	}
	fresh, _ = h.domains.Get(ctx, wsID, fresh.ID)
	stale, _ = h.domains.Get(ctx, wsID, old.ID)
	if fresh.Status != es.DomainVerifying || stale.ReleaseState != es.ReleaseDone || stale.AwaitsRelease() {
		t.Fatalf("after the run: new %+v old %+v", fresh, stale)
	}
	if !slices.Contains(h.routes.removed, old.ReplyDomain) || !slices.Contains(h.provider.removedIdents, old.Domain) || len(h.dns.deleted["envios.test"]) != len(old.Records) {
		t.Fatalf("old provider setup left behind: routes %v identities %v dns %v", h.routes.removed, h.provider.removedIdents, h.dns.deleted)
	}
	for _, call := range []string{"remove route", "remove identity"} {
		if got := h.routes.lanesOf(call); call == "remove route" && (len(got) != 1 || got[0] != es.LaneBackground) {
			t.Errorf("%s lanes = %v, want background", call, got)
		}
		if got := h.provider.lanesOf(call); call == "remove identity" && (len(got) != 1 || got[0] != es.LaneBackground) {
			t.Errorf("%s lanes = %v, want background", call, got)
		}
	}
	again, err := h.m.Setup.ChangeSubdomain(ctx, wsID, "cartaodetodososasco")
	if err != nil || len(h.wake.take()) != 0 || again.Account.Slug != "cartaodetodososasco" {
		t.Fatalf("the same name again changes nothing: %+v %v", again.Account, err)
	}
}

func TestTheSubdomainIsLockedAfterTheFirstSendFromIt(t *testing.T) {
	h, _ := readyHarness(t)
	ctx := context.Background()
	h.activity.sharedSends = map[string]bool{wsID: true}
	if _, err := h.m.Setup.ChangeSubdomain(ctx, wsID, "cartaodetodososasco"); !errors.Is(err, es.ErrSubdomainLocked) {
		t.Fatalf("err = %v", err)
	}
	if _, err := h.m.Setup.Provision(ctx, wsID, "cartaodetodososasco"); !errors.Is(err, es.ErrSubdomainLocked) {
		t.Fatalf("activation with another name = %v", err)
	}
	view, err := h.m.Setup.Status(ctx, wsID)
	if err != nil || !view.Subdomain.Locked || view.Subdomain.Slug != "loja-da-joana" {
		t.Fatalf("subdomain = %+v %v", view.Subdomain, err)
	}
}

func TestChangingTheSubdomainRefusesWhatActivationRefuses(t *testing.T) {
	h, _ := readyHarness(t)
	ctx := context.Background()
	if _, err := h.m.Setup.Provision(ctx, otherWorkspace, "outra-loja"); err != nil {
		t.Fatal(err)
	}
	for slug, want := range map[string]error{"outra-loja": es.ErrSlugTaken, "abuse": es.ErrSlugReserved, "Outra": es.ErrInvalidSlug} {
		if _, err := h.m.Setup.ChangeSubdomain(ctx, wsID, slug); !errors.Is(err, want) {
			t.Errorf("%s = %v, want %v", slug, err, want)
		}
	}
	if _, err := h.m.Setup.ChangeSubdomain(ctx, otherWorkspace+"x", "nova"); !errors.Is(err, es.ErrAccountNotProvisioned) {
		t.Fatalf("no account = %v", err)
	}
}

func TestAFailedReleaseStaysPendingUntilTheSweepFinishesIt(t *testing.T) {
	h, old := readyHarness(t)
	ctx := context.Background()
	h.routes.removeErr = errors.New("rule set busy")
	if _, err := h.m.Setup.ChangeSubdomain(ctx, wsID, "nova"); err != nil {
		t.Fatal(err)
	}
	if err := h.runSetup(); !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("run err = %v", err)
	}
	stale, _ := h.domains.Get(ctx, wsID, old.ID)
	if !stale.AwaitsRelease() {
		t.Fatalf("a failed release must stay pending: %+v", stale)
	}
	h.routes.removeErr = nil
	if err := h.m.Verification.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if stale, _ = h.domains.Get(ctx, wsID, old.ID); stale.ReleaseState != es.ReleaseDone {
		t.Fatalf("after the sweep = %+v", stale)
	}
}

func TestSwitchingBackToAFormerSubdomainReinstatesItsDomain(t *testing.T) {
	h, old := readyHarness(t)
	ctx := context.Background()
	if _, err := h.m.Setup.ChangeSubdomain(ctx, wsID, "nova"); err != nil {
		t.Fatal(err)
	}
	if err := h.runSetup(); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.Setup.ChangeSubdomain(ctx, wsID, "loja-da-joana"); err != nil {
		t.Fatal(err)
	}
	back, _ := h.domains.Get(ctx, wsID, old.ID)
	if back.Retired || back.ReleaseState != es.ReleaseNone || back.SetupProgress().State != es.SetupInProgress {
		t.Fatalf("former domain = %+v", back)
	}
	if err := h.runSetup(); err != nil {
		t.Fatal(err)
	}
	if back, _ = h.domains.Get(ctx, wsID, old.ID); back.Status != es.DomainVerifying {
		t.Fatalf("former domain after the run = %+v", back)
	}
	senders, _ := h.senders.ListByWorkspace(ctx, wsID)
	if len(senders) != 1 || senders[0].DomainID != old.ID {
		t.Fatalf("senders = %+v", senders)
	}
}

func TestActivatingAgainWithAnotherNameChangesTheSubdomain(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.provision(t, wsID)
	view, err := h.m.Setup.Provision(ctx, wsID, "nova")
	if err != nil || view.Account.Slug != "nova" {
		t.Fatalf("account = %+v %v", view.Account, err)
	}
	if err := h.runSetup(); err != nil {
		t.Fatal(err)
	}
	live := []string{}
	domains, _ := h.domains.ListByWorkspace(ctx, wsID)
	for _, d := range domains {
		if !d.Retired {
			live = append(live, d.Domain)
		}
	}
	if !slices.Equal(live, []string{"nova.envios.test"}) {
		t.Fatalf("live domains = %v", live)
	}
}

func TestPromotionKeepsTheChosenSubdomain(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.m.Setup.Provision(ctx, wsID, "cartaodetodososasco"); err != nil {
		t.Fatal(err)
	}
	if err := h.runSetup(); err != nil {
		t.Fatal(err)
	}
	probation, _ := h.m.c().domainOfKind(ctx, wsID, es.DomainSharedProbation)
	h.provider.states[probation.Domain] = es.IdentityState{DKIMVerified: true, MailFromVerified: true}
	if _, err := h.m.Domains.Recheck(ctx, wsID, probation.ID); err != nil {
		t.Fatal(err)
	}
	now := testNow.Add(15 * 24 * time.Hour)
	h.now = now
	if err := h.m.Promotion.Sweep(ctx, now); err != nil {
		t.Fatal(err)
	}
	trusted, err := h.m.c().domainOfKind(ctx, wsID, es.DomainSharedTrusted)
	if err != nil || trusted.Domain != "cartaodetodososasco.mail.test" {
		t.Fatalf("trusted = %+v %v", trusted, err)
	}
	h.provider.states[trusted.Domain] = es.IdentityState{DKIMVerified: true, MailFromVerified: true}
	if err := h.m.Verification.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	view, err := h.m.Setup.Status(ctx, wsID)
	if err != nil || view.Account.Mode != es.ModeTrusted || view.Subdomain.Address != "contato@cartaodetodososasco.mail.test" {
		t.Fatalf("after promotion = %+v %+v %v", view.Account, view.Subdomain, err)
	}
	if stale, _ := h.domains.Get(ctx, wsID, probation.ID); stale.AwaitsRelease() || stale.ReleaseState != es.ReleaseNone {
		t.Fatalf("promotion must not release the probation name: %+v", stale)
	}
}
