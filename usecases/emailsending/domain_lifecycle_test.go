package emailsending

import (
	"context"
	"errors"
	"testing"

	es "vozko/domain/emailsending"
)

func listedDomain(view StatusView, id string) (es.SendingDomain, bool) {
	for _, d := range view.Domains {
		if d.Domain.ID == id {
			return d.Domain, true
		}
	}
	return es.SendingDomain{}, false
}

func releasingDomain(t *testing.T, h *harness) *es.SendingDomain {
	t.Helper()
	d := &es.SendingDomain{WorkspaceID: wsID, Domain: "antiga.envios.test", Kind: es.DomainSharedProbation, Status: es.DomainVerified, ConnectMethod: es.ConnectAutomatic, ReplyDomain: "antiga.envios.test"}
	d.Release()
	if err := h.domains.Create(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestStatusListsAReleasingDomainUntilTheProviderIsCleaned(t *testing.T) {
	h, old := readyHarness(t)
	ctx := context.Background()
	if _, err := h.m.Setup.ChangeSubdomain(ctx, wsID, "cartaodetodososasco"); err != nil {
		t.Fatal(err)
	}
	view, err := h.m.Setup.Status(ctx, wsID)
	if err != nil {
		t.Fatal(err)
	}
	listed, ok := listedDomain(view, old.ID)
	if !ok || listed.Lifecycle() != es.LifecycleReleasing || listed.Actions() != (es.DomainActions{}) {
		t.Fatalf("the releasing domain must stay listed without actions: %+v %v", listed, ok)
	}
	if err := h.m.Setup.Complete(ctx, wsID); err != nil {
		t.Fatal(err)
	}
	view, err = h.m.Setup.Status(ctx, wsID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := listedDomain(view, old.ID); ok {
		t.Fatalf("a released domain must leave the list: %+v", view.Domains)
	}
}

func TestStatusHidesARemovedDomain(t *testing.T) {
	h, _ := readyHarness(t)
	ctx := context.Background()
	own := h.connect(t, wsID, "loja.com.br")
	if err := h.m.Domains.Remove(ctx, wsID, own.ID); err != nil {
		t.Fatal(err)
	}
	view, err := h.m.Setup.Status(ctx, wsID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := listedDomain(view, own.ID); ok {
		t.Fatalf("a removed domain must not be listed: %+v", view.Domains)
	}
}

func TestSendersCarryTheLifecycleOfTheirDomain(t *testing.T) {
	h, active := readyHarness(t)
	ctx := context.Background()
	releasing := releasingDomain(t, h)
	stranded := es.Sender{WorkspaceID: wsID, DomainID: releasing.ID, LocalPart: "vendas", DisplayName: "Vendas"}
	if err := h.senders.Create(ctx, &stranded); err != nil {
		t.Fatal(err)
	}
	view, err := h.m.Setup.Status(ctx, wsID)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := h.m.Senders.List(ctx, wsID)
	if err != nil {
		t.Fatal(err)
	}
	for name, senders := range map[string][]SenderView{"status": view.Senders, "list": listed} {
		lifecycles := map[string]es.DomainLifecycle{}
		for _, s := range senders {
			lifecycles[s.Sender.ID] = s.Domain.Lifecycle()
			if s.Sender.ID == stranded.ID && s.Address != "vendas@antiga.envios.test" {
				t.Fatalf("%s: address = %q", name, s.Address)
			}
		}
		if lifecycles[stranded.ID] != es.LifecycleReleasing || len(lifecycles) != 2 {
			t.Fatalf("%s: lifecycles = %v", name, lifecycles)
		}
		for id, lifecycle := range lifecycles {
			if id != stranded.ID && lifecycle != es.LifecycleActive {
				t.Fatalf("%s: the sender on %s is %q", name, active.Domain, lifecycle)
			}
		}
	}
}

func TestSendersRefuseAReleasingDomain(t *testing.T) {
	h, _ := readyHarness(t)
	ctx := context.Background()
	releasing := releasingDomain(t, h)
	if _, err := h.m.Senders.Create(ctx, wsID, CreateSenderInput{DomainID: releasing.ID, LocalPart: "vendas", DisplayName: "Vendas"}); !errors.Is(err, es.ErrDomainReleasing) {
		t.Fatalf("create err = %v", err)
	}
	senders, err := h.m.Senders.List(ctx, wsID)
	if err != nil || len(senders) != 1 {
		t.Fatalf("senders = %+v %v", senders, err)
	}
	if _, err := h.m.Senders.Update(ctx, wsID, senders[0].Sender.ID, UpdateSenderInput{DomainID: &releasing.ID}); !errors.Is(err, es.ErrDomainReleasing) {
		t.Fatalf("update err = %v", err)
	}
}
