package emailsending

import (
	"errors"
	"testing"
)

func TestTheDomainLifecycleFollowsTheRelease(t *testing.T) {
	active := SendingDomain{Kind: DomainOwn, Status: DomainVerified}
	releasing := SendingDomain{Kind: DomainSharedProbation, Status: DomainVerified}
	releasing.Release()
	released := releasing
	released.Released()
	removed := SendingDomain{Kind: DomainOwn, Status: DomainVerified, Retired: true}
	cases := []struct {
		name   string
		domain SendingDomain
		want   DomainLifecycle
		listed bool
	}{
		{name: "active", domain: active, want: LifecycleActive, listed: true},
		{name: "release pending", domain: releasing, want: LifecycleReleasing, listed: true},
		{name: "released", domain: released, want: LifecycleRetired},
		{name: "removed", domain: removed, want: LifecycleRetired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.domain.Lifecycle(); got != tc.want {
				t.Fatalf("lifecycle = %q, want %q", got, tc.want)
			}
			if got := tc.domain.Listed(); got != tc.listed {
				t.Fatalf("listed = %v, want %v", got, tc.listed)
			}
		})
	}
}

func TestOnlyAnActiveDomainOffersActions(t *testing.T) {
	own := SendingDomain{Kind: DomainOwn, Status: DomainVerifying}
	shared := SendingDomain{Kind: DomainSharedProbation, Status: DomainVerified}
	releasing := SendingDomain{Kind: DomainOwn, Status: DomainVerified}
	releasing.Release()
	removed := SendingDomain{Kind: DomainOwn, Status: DomainVerified, Retired: true}
	cases := []struct {
		name   string
		domain SendingDomain
		want   DomainActions
	}{
		{name: "own domain", domain: own, want: DomainActions{Recheck: true, Remove: true}},
		{name: "shared domain", domain: shared, want: DomainActions{Recheck: true}},
		{name: "releasing domain", domain: releasing},
		{name: "removed domain", domain: removed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.domain.Actions(); got != tc.want {
				t.Fatalf("actions = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestAReleasingDomainTakesNoSenders(t *testing.T) {
	d := SendingDomain{ID: "d1", WorkspaceID: "w1", Domain: "loja.envios.test", Kind: DomainSharedProbation, Status: DomainVerified}
	d.Release()
	if _, err := NewSender(d, "contato", "Loja", ""); !errors.Is(err, ErrDomainReleasing) {
		t.Fatalf("err = %v, want %v", err, ErrDomainReleasing)
	}
	if d.Sendable() {
		t.Fatal("a releasing domain must not be sendable")
	}
}
