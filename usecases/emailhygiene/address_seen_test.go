package emailhygiene

import (
	"context"
	"slices"
	"testing"

	"vozko/domain/emailaddress"
	h "vozko/domain/emailhygiene"
)

var _ h.AddressSeen = (*Service)(nil)

func TestASeenAddressQueuesItsDomainCheckWithoutResolvingIt(t *testing.T) {
	svc, checks := sendRefusalService(t, nil)
	resolver := svc.d.Domains.d.Resolver.(*fakeResolver)
	svc.AddressesSeen(context.Background(), []emailaddress.Address{"ana@nova.com.br", "bia@nova.com.br", "caio@outra.com.br"})
	slices.Sort(checks.requested)
	if !slices.Equal(checks.requested, []string{"nova.com.br", "outra.com.br"}) {
		t.Fatalf("requested %v, want each seen domain queued once", checks.requested)
	}
	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	if len(resolver.calls) != 0 {
		t.Fatalf("seeing an address resolved %v inline, the durable queue does that", resolver.calls)
	}
}

func TestSeeingNoAddressQueuesNothing(t *testing.T) {
	svc, checks := sendRefusalService(t, nil)
	svc.AddressesSeen(context.Background(), nil)
	if len(checks.requested) != 0 {
		t.Fatalf("requested %v", checks.requested)
	}
}
