package emailsending

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"vozko/domain/emailaddress"
	es "vozko/domain/emailsending"
)

func (h *harness) idleAccount(t *testing.T) es.SenderAccount {
	t.Helper()
	h.provisionVerified(t, wsID)
	acct, err := h.accounts.GetByWorkspace(context.Background(), wsID)
	if err != nil {
		t.Fatal(err)
	}
	long := h.now.Add(-es.DormancyAfter - time.Hour)
	acct.CreatedAt, acct.UpdatedAt = long, long
	h.accounts.rows[acct.ID] = *acct
	return *acct
}

func (h *harness) dormantAccount(t *testing.T) es.SenderAccount {
	t.Helper()
	h.idleAccount(t)
	if err := h.m.Dormancy.Sweep(context.Background(), h.now); err != nil {
		t.Fatal(err)
	}
	return h.stored(t)
}

func (h *harness) stored(t *testing.T) es.SenderAccount {
	t.Helper()
	acct, err := h.accounts.GetByWorkspace(context.Background(), wsID)
	if err != nil {
		t.Fatal(err)
	}
	return *acct
}

func TestSweepRetiresAnIdleAccountKeepingItsIdentities(t *testing.T) {
	h := newHarness(t)
	acct := h.idleAccount(t)
	if err := h.m.Dormancy.Sweep(context.Background(), h.now); err != nil {
		t.Fatal(err)
	}
	if got := h.stored(t); got.Status != es.AccountDormant {
		t.Fatalf("status = %s, want dormant", got.Status)
	}
	if len(h.tenants.retired) != 1 || h.tenants.retired[0].Name != acct.TenantName {
		t.Fatalf("retired = %+v", h.tenants.retired)
	}
	domains, _ := h.domains.ListByWorkspace(context.Background(), wsID)
	if len(h.tenants.retired[0].Identities) != 1 || h.tenants.retired[0].Identities[0] != domains[0].Domain {
		t.Fatalf("identities = %v, want the verified domain %s", h.tenants.retired[0].Identities, domains[0].Domain)
	}
	if len(h.provider.removedIdents) != 0 {
		t.Fatalf("retirement keeps identities, removed %v", h.provider.removedIdents)
	}
}

func TestSweepKeepsAccountsThatAreNotIdle(t *testing.T) {
	cases := []struct {
		name    string
		arrange func(h *harness)
	}{
		{name: "a recent send", arrange: func(h *harness) { h.activity.lastSent[wsID] = h.now.AddDate(0, 0, -10) }},
		{name: "a live campaign", arrange: func(h *harness) { h.live.live[wsID] = true }},
		{name: "a recent change to the account", arrange: func(h *harness) {
			acct, _ := h.accounts.GetByWorkspace(context.Background(), wsID)
			acct.UpdatedAt = h.now.AddDate(0, 0, -1)
			h.accounts.rows[acct.ID] = *acct
		}},
		{name: "a paused account", arrange: func(h *harness) {
			acct, _ := h.accounts.GetByWorkspace(context.Background(), wsID)
			acct.Status = es.AccountPausedByProvider
			h.accounts.rows[acct.ID] = *acct
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.idleAccount(t)
			tc.arrange(h)
			if err := h.m.Dormancy.Sweep(context.Background(), h.now); err != nil {
				t.Fatal(err)
			}
			if len(h.tenants.retired) != 0 {
				t.Fatalf("retired = %+v", h.tenants.retired)
			}
			if got := h.stored(t); got.Status == es.AccountDormant {
				t.Fatalf("status = %s", got.Status)
			}
		})
	}
}

func TestSweepThatCannotReadActivityRetiresNothing(t *testing.T) {
	cases := []struct {
		name    string
		arrange func(h *harness)
	}{
		{name: "last send unreadable", arrange: func(h *harness) { h.activity.err = errors.New("db down") }},
		{name: "campaigns unreadable", arrange: func(h *harness) { h.live.err = errors.New("db down") }},
		{name: "lock unavailable", arrange: func(h *harness) { h.locks.err = errors.New("redis down") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.idleAccount(t)
			tc.arrange(h)
			if err := h.m.Dormancy.Sweep(context.Background(), h.now); err == nil {
				t.Fatal("expected the sweep to report the failure")
			}
			if len(h.tenants.retired) != 0 || h.stored(t).Status != es.AccountActive {
				t.Fatalf("retired %+v status %s", h.tenants.retired, h.stored(t).Status)
			}
		})
	}
}

func TestSweepFailureRestoresTheTenantKeepsTheAccountActiveAndAlertsOps(t *testing.T) {
	h := newHarness(t)
	h.idleAccount(t)
	h.tenants.retireErr = errors.New("throttled")
	if err := h.m.Dormancy.Sweep(context.Background(), h.now); err == nil {
		t.Fatal("expected the failure")
	}
	if got := h.stored(t); got.Status != es.AccountActive {
		t.Fatalf("status = %s, want active", got.Status)
	}
	if h.tenants.revivals() != 1 {
		t.Fatalf("a half retired tenant must be restored, revivals = %d", h.tenants.revivals())
	}
	if len(h.ops.alerts) == 0 {
		t.Fatal("ops must hear about a failed retirement")
	}
	h.tenants.retireErr = nil
	if err := h.m.Dormancy.Sweep(context.Background(), h.now); err != nil {
		t.Fatalf("the next sweep retries: %v", err)
	}
	if got := h.stored(t); got.Status != es.AccountDormant {
		t.Fatalf("status after the retry = %s, want dormant", got.Status)
	}
}

func TestSweepWhoseRestoreAlsoFailsLeavesTheAccountDormantSoTheNextSendRebuildsIt(t *testing.T) {
	h := newHarness(t)
	h.idleAccount(t)
	h.tenants.retireErr = errors.New("throttled")
	h.tenants.reviveErr = errors.New("throttled")
	if err := h.m.Dormancy.Sweep(context.Background(), h.now); err == nil {
		t.Fatal("expected the failure")
	}
	if got := h.stored(t); got.Status != es.AccountDormant {
		t.Fatalf("status = %s, want dormant", got.Status)
	}
	if len(h.ops.alerts) == 0 {
		t.Fatal("ops must hear about it")
	}
}

func TestGatewayRevivesADormantAccountThenSends(t *testing.T) {
	h := newHarness(t)
	dormant := h.dormantAccount(t)
	if _, err := h.m.Suppressions.Add(context.Background(), wsID, "bloqueado@example.com"); err != nil {
		t.Fatal(err)
	}
	msg, err := h.m.Gateway.Send(context.Background(), marketingRequest(), compose("Oi"))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Status != es.MessageSent {
		t.Fatalf("message = %+v", msg)
	}
	got := h.stored(t)
	if got.Status != es.AccountActive || got.TenantRef != "arn:tenant/"+dormant.TenantName+"/revived" {
		t.Fatalf("account = %+v", got)
	}
	if h.tenants.revivals() != 1 || len(h.tenants.revived[0].Identities) != 1 {
		t.Fatalf("revived = %+v", h.tenants.revived)
	}
	if len(h.provider.suppressed) != 0 {
		t.Fatalf("revive makes no per-address provider calls, pushed %v", h.provider.suppressed)
	}
}

func TestASuppressedAddressIsStillRefusedAfterReviveBecauseTheGatewayChecksOurList(t *testing.T) {
	cases := []struct {
		name     string
		suppress func(h *harness) error
	}{
		{name: "bounce recorded before retirement", suppress: func(h *harness) error {
			return h.suppressions.Suppress(context.Background(), es.Suppression{WorkspaceID: wsID, Address: "bloqueado@example.com", Reason: es.SuppressHardBounce})
		}},
		{name: "manual block on the dormant account", suppress: func(h *harness) error {
			_, err := h.m.Suppressions.Add(context.Background(), wsID, "bloqueado@example.com")
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.dormantAccount(t)
			ctx := context.Background()
			if err := tc.suppress(h); err != nil {
				t.Fatal(err)
			}
			if _, err := h.m.Gateway.Send(ctx, marketingRequest(), compose("Oi")); err != nil {
				t.Fatalf("revive and send to someone else: %v", err)
			}
			if got := h.stored(t); got.Status != es.AccountActive {
				t.Fatalf("status = %s", got.Status)
			}
			req := marketingRequest()
			req.To = emailaddress.Address("bloqueado@example.com")
			if _, err := h.m.Gateway.Send(ctx, req, compose("Oi")); !errors.Is(err, es.ErrRecipientSuppressed) {
				t.Fatalf("our own list still refuses the address, err = %v", err)
			}
			if len(h.provider.suppressed) != 0 || len(h.provider.sent) != 1 {
				t.Fatalf("pushed %v sent %d", h.provider.suppressed, len(h.provider.sent))
			}
		})
	}
}

func TestConcurrentSendsReviveOnce(t *testing.T) {
	h := newHarness(t)
	h.dormantAccount(t)
	h.tenants.reviveWait = 20 * time.Millisecond
	const senders = 8
	var wg sync.WaitGroup
	errs := make(chan error, senders)
	for range senders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := h.m.Gateway.Send(context.Background(), marketingRequest(), compose("Oi"))
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if h.tenants.revivals() != 1 {
		t.Fatalf("revivals = %d, want exactly one", h.tenants.revivals())
	}
}

func TestAFailedRevivalRefusesTheSendWithACodedErrorAndSendsNothing(t *testing.T) {
	h := newHarness(t)
	h.dormantAccount(t)
	h.tenants.reviveErr = errors.New("ses down")
	_, err := h.m.Gateway.Send(context.Background(), marketingRequest(), compose("Oi"))
	if !errors.Is(err, es.ErrAccountReactivation) {
		t.Fatalf("err = %v, want ErrAccountReactivation", err)
	}
	if len(h.provider.sent) != 0 || len(h.messages.rows) != 0 {
		t.Fatalf("nothing is sent or recorded: sent %d rows %d", len(h.provider.sent), len(h.messages.rows))
	}
	if got := h.stored(t); got.Status != es.AccountDormant {
		t.Fatalf("status = %s, want still dormant", got.Status)
	}
	if len(h.ops.alerts) == 0 {
		t.Fatal("ops must hear about a failed revival")
	}
}

func TestReactivateLeavesAnActiveAccountAlone(t *testing.T) {
	h := newHarness(t)
	h.provisionVerified(t, wsID)
	if err := h.m.Reactivation.Reactivate(context.Background(), wsID); err != nil {
		t.Fatal(err)
	}
	if h.tenants.revivals() != 0 {
		t.Fatalf("revivals = %d", h.tenants.revivals())
	}
}

func TestReactivateWakesADormantAccount(t *testing.T) {
	h := newHarness(t)
	h.dormantAccount(t)
	if err := h.m.Reactivation.Reactivate(context.Background(), wsID); err != nil {
		t.Fatal(err)
	}
	if got := h.stored(t); got.Status != es.AccountActive || h.tenants.revivals() != 1 {
		t.Fatalf("status = %s revivals = %d", got.Status, h.tenants.revivals())
	}
}

func TestDormantAccountsSkipProviderHealthChecks(t *testing.T) {
	h := newHarness(t)
	h.dormantAccount(t)
	h.provider.healthErr = errors.New("tenant does not exist")
	views, _, err := h.m.Admin.List(context.Background(), 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].Account.Status != es.AccountDormant || views[0].Health != nil || views[0].HealthError != "" {
		t.Fatalf("view = %+v", views)
	}
	if err := h.m.Promotion.Sweep(context.Background(), h.now); err != nil {
		t.Fatalf("the promotion sweep skips dormant accounts: %v", err)
	}
}

func TestSetupActionsOnADormantAccountReviveItFirst(t *testing.T) {
	cases := []struct {
		name       string
		background bool
		act        func(h *harness) error
	}{
		{name: "provision", background: true, act: func(h *harness) error {
			_, err := h.m.Setup.Provision(context.Background(), wsID, "")
			return err
		}},
		{name: "connect an own domain", background: true, act: func(h *harness) error {
			_, err := h.m.Domains.Connect(context.Background(), wsID, "loja.com.br")
			return err
		}},
		{name: "admin promote", background: true, act: func(h *harness) error {
			_, err := h.m.Admin.Promote(context.Background(), wsID)
			return err
		}},
		{name: "admin pause", act: func(h *harness) error {
			_, err := h.m.Admin.Pause(context.Background(), wsID, "")
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.dormantAccount(t)
			if err := tc.act(h); err != nil {
				t.Fatal(err)
			}
			if tc.background {
				if h.tenants.revivals() != 0 {
					t.Fatalf("the request revived the tenant itself: %d", h.tenants.revivals())
				}
				if err := h.runSetup(); err != nil {
					t.Fatal(err)
				}
			}
			if h.tenants.revivals() != 1 {
				t.Fatalf("revivals = %d, want the tenant back before touching it", h.tenants.revivals())
			}
			if got := h.stored(t); tc.background && got.Status != es.AccountActive {
				t.Fatalf("status = %s", got.Status)
			}
		})
	}
}

func TestVerificationRetryWaitsForADormantAccountToWake(t *testing.T) {
	h := newHarness(t)
	h.dormantAccount(t)
	pending := &es.SendingDomain{WorkspaceID: wsID, Domain: "loja.com.br", Kind: es.DomainOwn, Status: es.DomainPending, ConnectMethod: es.ConnectManual}
	if err := h.domains.Create(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
	identities := len(h.provider.identities)
	if err := h.m.Verification.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.provider.identities) != identities || h.tenants.revivals() != 0 {
		t.Fatalf("a background retry must not touch a retired tenant: identities %d revivals %d", len(h.provider.identities)-identities, h.tenants.revivals())
	}
}
