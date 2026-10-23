package emailhygiene

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/emailaddress"
	h "vozko/domain/emailhygiene"
	es "vozko/domain/emailsending"
)

func sendRefusalService(t *testing.T, rows map[emailaddress.Address]h.Standing) (*Service, *fakeChecks) {
	t.Helper()
	checks := newChecks()
	disposable, err := NewDisposable(DisposableDeps{Store: &fakeLists{}, Source: fakeSource{}}, listWith("mailinator.com"))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(ServiceDeps{Standing: fakeStanding{rows: rows}, Domains: newDomains(newResolver(), checks, &fakeKick{}), Disposable: disposable})
	svc.now = func() time.Time { return clock }
	return svc, checks
}

func TestAOneToOneSendIsRefusedByTheCampaignRuleAndQueuesAnUncheckedDomain(t *testing.T) {
	svc, checks := sendRefusalService(t, map[emailaddress.Address]h.Standing{
		"ana@loja.com.br":   {Domain: h.DomainAccepts},
		"x@lento.com.br":    {Domain: h.DomainUnknown},
		"stale@loja.com.br": {Domain: h.DomainAccepts, DomainStale: true},
	})
	ctx := context.Background()
	cases := map[emailaddress.Address]string{
		"ana@loja.com.br":   "",
		"stale@loja.com.br": "",
		"y@mailinator.com":  h.CodeDisposableAddress,
		"x@lento.com.br":    h.CodeDomainUnknown,
		"novo@nova.com.br":  h.CodeDomainChecking,
	}
	for address, want := range cases {
		err := svc.SendRefusal(ctx, "ws", es.RunReply, address)
		code, _ := h.RecipientRefusalCode(err)
		if code != want || (want == "" && err != nil) {
			t.Fatalf("%s: err = %v, want code %q", address, err, want)
		}
	}
	queued := map[string]bool{}
	for _, d := range checks.requested {
		queued[d] = true
	}
	if !queued["nova.com.br"] || !queued["loja.com.br"] || queued["lento.com.br"] {
		t.Fatalf("requested %v, an unchecked or stale domain is queued and a settled one is not", checks.requested)
	}
}

func TestTheTransactionalAPIKeepsItsLenientRule(t *testing.T) {
	svc, checks := sendRefusalService(t, nil)
	for _, address := range []emailaddress.Address{"y@mailinator.com", "novo@nova.com.br"} {
		if err := svc.SendRefusal(context.Background(), "ws", es.RunAPI, address); err != nil {
			t.Fatalf("%s refused for the API: %v", address, err)
		}
	}
	if len(checks.requested) != 0 {
		t.Fatalf("the API path reads nothing, requested %v", checks.requested)
	}
}

type brokenStanding struct{}

func (brokenStanding) Standing(context.Context, string, []emailaddress.Address) (map[emailaddress.Address]h.Standing, error) {
	return nil, errors.New("db down")
}

func TestAOneToOneSendFailsClosedWhenTheStandingIsUnreadable(t *testing.T) {
	svc := NewService(ServiceDeps{Standing: brokenStanding{}})
	if err := svc.SendRefusal(context.Background(), "ws", es.RunTriggered, "ana@loja.com.br"); err == nil {
		t.Fatal("an unreadable standing must refuse the send")
	}
}
