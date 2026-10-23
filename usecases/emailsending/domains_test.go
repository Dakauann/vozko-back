package emailsending

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	es "vozko/domain/emailsending"
)

func provisioned(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.provision(t, wsID)
	return h
}

func TestConnectRefusals(t *testing.T) {
	cases := []struct {
		name   string
		domain string
		ws     string
		want   error
	}{
		{"invalid domain", "not a domain", wsID, ErrDomainInvalid},
		{"free mailbox", "gmail.com", wsID, ErrDomainFreeMailbox},
		{"shared probation", "envios.test", wsID, ErrDomainShared},
		{"subdomain of shared", "x.mail.test", wsID, ErrDomainShared},
		{"parent of shared", "test", wsID, ErrDomainInvalid},
		{"not provisioned", "loja.com.br", "22222222-2222-2222-2222-222222222222", es.ErrAccountNotProvisioned},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := provisioned(t)
			if _, err := h.m.Domains.Connect(context.Background(), tc.ws, tc.domain); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestConnectRefusesDomainOfAnotherWorkspace(t *testing.T) {
	h := provisioned(t)
	ctx := context.Background()
	other := "22222222-2222-2222-2222-222222222222"
	if _, err := h.m.Setup.Provision(ctx, other, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.Domains.Connect(ctx, wsID, "loja.com.br"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.Domains.Connect(ctx, other, "Loja.com.br."); !errors.Is(err, es.ErrDomainTaken) {
		t.Fatalf("err = %v", err)
	}
	if _, err := h.m.Domains.Connect(ctx, other, "loja-da-joana.envios.test"); !errors.Is(err, ErrDomainShared) {
		t.Fatalf("err = %v", err)
	}
}

func TestConnectAnswersRightAwayWithTheSetupInProgress(t *testing.T) {
	h := provisioned(t)
	identities := len(h.provider.identities)
	res, err := h.m.Domains.Connect(context.Background(), wsID, "Loja.com.br")
	if err != nil {
		t.Fatal(err)
	}
	if res.Domain != "loja.com.br" || res.Kind != es.DomainOwn || res.Status != es.DomainPending || res.SetupProgress().State != es.SetupInProgress || len(res.Records) != 0 {
		t.Fatalf("domain = %+v", res)
	}
	if len(h.provider.identities) != identities || len(h.routes.ensured) != 1 {
		t.Fatalf("the request touched the provider: identities %v routes %v", h.provider.identities, h.routes.ensured)
	}
	if kicks := h.wake.take(); len(kicks) != 1 || kicks[0] != wsID {
		t.Fatalf("kicks = %v", kicks)
	}
}

func TestTheSetupRunAddsTheOwnDomainRecords(t *testing.T) {
	h := provisioned(t)
	d := h.connect(t, wsID, "Loja.com.br")
	if d.Domain != "loja.com.br" || d.Kind != es.DomainOwn || d.Status != es.DomainVerifying || d.ConnectMethod != es.ConnectManual || d.ReplyDomain != "reply.loja.com.br" {
		t.Fatalf("domain = %+v", d)
	}
	purposes := map[es.RecordPurpose]int{}
	for _, r := range d.Records {
		purposes[r.Purpose]++
	}
	if purposes[es.RecordDMARC] != 1 || purposes[es.RecordReplyMX] != 1 || purposes[es.RecordDKIM] != 1 {
		t.Fatalf("records = %+v", d.Records)
	}
	if len(h.dns.upserts["envios.test"]) != 4 {
		t.Fatal("connecting an own domain must not touch the shared zones")
	}
	if h.routes.ensured[len(h.routes.ensured)-1] != "reply.loja.com.br" {
		t.Fatalf("inbound = %v", h.routes.ensured)
	}
}

func TestConnectKeepsAnExistingDMARC(t *testing.T) {
	h := provisioned(t)
	h.dmarc.value = "v=DMARC1; p=reject"
	res := h.connect(t, wsID, "loja.com.br")
	for _, r := range res.Records {
		if r.Purpose == es.RecordDMARC {
			t.Fatal("an existing dmarc record must be kept")
		}
	}
	if res.ConnectMethod != es.ConnectManual {
		t.Fatalf("connect method = %q", res.ConnectMethod)
	}
}

func TestADMARCLookupFailureIsRecordedOnTheDomain(t *testing.T) {
	h := provisioned(t)
	ctx := context.Background()
	h.dmarc.err = errors.New("servfail")
	res, err := h.m.Domains.Connect(ctx, wsID, "loja.com.br")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.runSetup(); !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("err = %v", err)
	}
	d, _ := h.domains.Get(ctx, wsID, res.ID)
	if d.SetupProgress() != (es.SetupProgress{State: es.SetupFailed, Failure: es.SetupFailureProvider}) || !strings.HasPrefix(d.FailureReason, "dmarc lookup: ") {
		t.Fatalf("domain = %+v", d)
	}
}

func TestRecheckOfADomainStillBeingSetUpRestartsTheSetup(t *testing.T) {
	h := provisioned(t)
	ctx := context.Background()
	h.dmarc.err = errors.New("servfail")
	res, err := h.m.Domains.Connect(ctx, wsID, "loja.com.br")
	if err != nil {
		t.Fatal(err)
	}
	_ = h.runSetup()
	h.dmarc.err = nil
	calls := h.provider.statusCalls
	d, err := h.m.Domains.Recheck(ctx, wsID, res.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.SetupProgress().State != es.SetupInProgress || d.FailureReason != "" || h.provider.statusCalls != calls {
		t.Fatalf("recheck of a pending domain = %+v, status calls %d", d, h.provider.statusCalls-calls)
	}
	if err := h.runSetup(); err != nil {
		t.Fatal(err)
	}
	if d, _ = h.domains.Get(ctx, wsID, res.ID); d.Status != es.DomainVerifying || len(d.Records) == 0 {
		t.Fatalf("after the run = %+v", d)
	}
}

func TestReconnectingAFailedDomainRestartsItsSetup(t *testing.T) {
	h := provisioned(t)
	ctx := context.Background()
	res := h.connect(t, wsID, "loja.com.br")
	h.provider.states["loja.com.br"] = es.IdentityState{Failed: true, FailureReason: "dkim_failed"}
	if d, err := h.m.Domains.Recheck(ctx, wsID, res.ID); err != nil || d.Status != es.DomainFailed {
		t.Fatalf("recheck = %+v %v", d, err)
	}
	h.provider.states["loja.com.br"] = es.IdentityState{}
	again, err := h.m.Domains.Connect(ctx, wsID, "loja.com.br")
	if err != nil || again.ID != res.ID || again.SetupProgress().State != es.SetupInProgress {
		t.Fatalf("reconnect = %+v %v", again, err)
	}
	if err := h.runSetup(); err != nil {
		t.Fatal(err)
	}
	if d, _ := h.domains.Get(ctx, wsID, res.ID); d.Status != es.DomainVerifying || d.FailureReason != "" {
		t.Fatalf("after the run = %+v", d)
	}
}

func TestRecheckOwnDomainVerification(t *testing.T) {
	h := provisioned(t)
	ctx := context.Background()
	res := h.connect(t, wsID, "loja.com.br")
	d, err := h.m.Domains.Recheck(ctx, wsID, res.ID)
	if err != nil || d.Status != es.DomainVerifying {
		t.Fatalf("pending recheck = %+v %v", d, err)
	}
	h.provider.states["loja.com.br"] = es.IdentityState{DKIMVerified: true, MailFromVerified: true}
	h.now = testNow.Add(time.Hour)
	d, err = h.m.Domains.Recheck(ctx, wsID, res.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != es.DomainVerified || d.VerifiedAt == nil || !d.VerifiedAt.Equal(h.now) {
		t.Fatalf("domain = %+v", d)
	}
	acct, _ := h.accounts.GetByWorkspace(ctx, wsID)
	if acct.Mode != es.ModeOwnDomain {
		t.Fatalf("mode = %q", acct.Mode)
	}
	found := false
	for _, k := range h.alerts.keys() {
		if len(k) > 21 && k[:21] == "email_domain_verified" {
			found = true
		}
	}
	if !found {
		t.Fatalf("drafts = %v", h.alerts.keys())
	}
}

func TestRecheckFailuresAndTimeout(t *testing.T) {
	t.Run("provider failure", func(t *testing.T) {
		h := provisioned(t)
		ctx := context.Background()
		res := h.connect(t, wsID, "loja.com.br")
		h.provider.states["loja.com.br"] = es.IdentityState{Failed: true, FailureReason: "dkim_failed"}
		d, err := h.m.Domains.Recheck(ctx, wsID, res.ID)
		if err != nil || d.Status != es.DomainFailed || d.FailureReason != "dkim_failed" {
			t.Fatalf("domain = %+v err = %v", d, err)
		}
		if keys := h.alerts.keys(); keys[len(keys)-1] != "email_domain_failed:"+d.ID {
			t.Fatalf("drafts = %v", keys)
		}
		h.provider.states["loja.com.br"] = es.IdentityState{}
		if d, _ = h.m.Domains.Recheck(ctx, wsID, res.ID); d.Status != es.DomainFailed {
			t.Fatalf("a failed domain only leaves failed when verified: %+v", d)
		}
		h.provider.states["loja.com.br"] = es.IdentityState{DKIMVerified: true, MailFromVerified: true}
		if d, _ = h.m.Domains.Recheck(ctx, wsID, res.ID); d.Status != es.DomainVerified {
			t.Fatalf("verification recovers a failed domain: %+v", d)
		}
	})
	t.Run("72 hours without verification", func(t *testing.T) {
		h := provisioned(t)
		ctx := context.Background()
		res := h.connect(t, wsID, "loja.com.br")
		h.now = testNow.Add(VerificationTimeout + time.Minute)
		if err := h.m.Verification.Sweep(ctx); err != nil {
			t.Fatal(err)
		}
		d, _ := h.domains.Get(ctx, wsID, res.ID)
		if d.Status != es.DomainFailed || d.FailureReason != "verification_timeout" {
			t.Fatalf("domain = %+v", d)
		}
	})
}

func TestRemoveDomain(t *testing.T) {
	h := provisioned(t)
	ctx := context.Background()
	shared, _ := h.m.c().domainOfKind(ctx, wsID, es.DomainSharedProbation)
	if err := h.m.Domains.Remove(ctx, wsID, shared.ID); !errors.Is(err, ErrSharedDomainRemoval) {
		t.Fatalf("err = %v", err)
	}
	res := h.connect(t, wsID, "loja.com.br")
	h.provider.states["loja.com.br"] = es.IdentityState{DKIMVerified: true, MailFromVerified: true}
	if _, err := h.m.Domains.Recheck(ctx, wsID, res.ID); err != nil {
		t.Fatal(err)
	}
	view, err := h.m.Senders.Create(ctx, wsID, CreateSenderInput{DomainID: res.ID, LocalPart: "vendas", DisplayName: "Loja"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.m.Domains.Remove(ctx, wsID, res.ID); !errors.Is(err, ErrDomainInUse) {
		t.Fatalf("err = %v", err)
	}
	if err := h.m.Senders.Delete(ctx, wsID, view.Sender.ID); err != nil {
		t.Fatal(err)
	}
	if err := h.m.Domains.Remove(ctx, wsID, res.ID); err != nil {
		t.Fatal(err)
	}
	d, _ := h.domains.Get(ctx, wsID, res.ID)
	if !d.Retired || h.provider.removedIdents[0] != "loja.com.br" || h.routes.removed[0] != "reply.loja.com.br" {
		t.Fatalf("domain = %+v removed = %v %v", d, h.provider.removedIdents, h.routes.removed)
	}
	acct, _ := h.accounts.GetByWorkspace(ctx, wsID)
	if acct.Mode != es.ModeProbation {
		t.Fatalf("mode after removing the only own domain = %q", acct.Mode)
	}
	again, err := h.m.Domains.Connect(ctx, wsID, "loja.com.br")
	if err != nil || again.Retired || again.ID != res.ID {
		t.Fatalf("reconnect = %+v err = %v", again, err)
	}
}
