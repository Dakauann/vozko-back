package emailhygiene

import (
	"encoding/hex"
	"testing"
	"time"

	"vozko/domain/emailaddress"
	es "vozko/domain/emailsending"
)

func TestHashOfIsTheSHA256OfTheNormalizedAddress(t *testing.T) {
	h := HashOf(emailaddress.Address("ana@loja.com.br"))
	if h.Hex() != "85b10a22944c081c6ce5e5e15f75432ed5460f914c8fae32ad43ef4e9f831ca5" {
		t.Fatalf("hex = %q", h.Hex())
	}
	raw, err := hex.DecodeString(h.Hex())
	if err != nil || len(raw) != 32 {
		t.Fatalf("a hash is 32 bytes, got %d (%v)", len(raw), err)
	}
	if HashOf("ana@loja.com.br") != HashOf("ana@loja.com.br") {
		t.Fatal("the same address hashes the same")
	}
	if HashOf("ana@loja.com.br") == HashOf("ana+promo@loja.com.br") {
		t.Fatal("a plus tag is a different address")
	}
}

func TestRefusalIsTheRuleEverySendShares(t *testing.T) {
	cases := []struct {
		name     string
		standing Standing
		want     es.SuppressReason
	}{
		{name: "a clean address is not refused", standing: Standing{Domain: DomainAccepts}, want: ""},
		{name: "an unchecked domain is not refused for one-to-one sends", standing: Standing{Domain: DomainPending}, want: ""},
		{name: "an unknown domain is not refused for one-to-one sends", standing: Standing{Domain: DomainUnknown}, want: ""},
		{name: "a workspace suppression keeps its reason", standing: Standing{Suppression: es.SuppressComplaint, BadAddress: true, Domain: DomainNoMail}, want: es.SuppressComplaint},
		{name: "a hard bounce anywhere refuses everywhere", standing: Standing{BadAddress: true, Domain: DomainAccepts}, want: es.SuppressBadAddress},
		{name: "a domain that refuses mail refuses the send", standing: Standing{Domain: DomainNoMail}, want: es.SuppressNoMailDomain},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.standing.Refusal(); got != tc.want {
				t.Fatalf("Refusal = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAssessOrdersVerdictsByWhatMattersMost(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		facts    Facts
		standing Standing
		want     Verdict
	}{
		{name: "a clean address is valid", standing: Standing{Domain: DomainAccepts}, want: VerdictValid},
		{name: "a workspace suppression comes first", facts: Facts{Disposable: true}, standing: Standing{Suppression: es.SuppressManual, BadAddress: true, Domain: DomainNoMail}, want: VerdictSuppressed},
		{name: "the shared bad list comes before the domain", standing: Standing{BadAddress: true, Domain: DomainNoMail}, want: VerdictBadAddress},
		{name: "a domain without mail comes before disposable", facts: Facts{Disposable: true}, standing: Standing{Domain: DomainNoMail}, want: VerdictNoMailDomain},
		{name: "disposable comes before an unknown domain", facts: Facts{Disposable: true}, standing: Standing{Domain: DomainUnknown}, want: VerdictDisposable},
		{name: "an unknown domain is unknown, never valid", standing: Standing{Domain: DomainUnknown}, want: VerdictDomainUnknown},
		{name: "an unchecked domain is pending", standing: Standing{Domain: DomainPending}, want: VerdictDomainPending},
		{name: "a sunset contact is inactive", standing: Standing{Domain: DomainAccepts, Activity: Activity{Known: true, SunsetSince: at(now.AddDate(-1, 0, 0)), CampaignsSinceEngaged: 9}}, want: VerdictInactive},
		{name: "a typo is a warning after the blocking verdicts", facts: Facts{Suggestion: "gmail.com", Role: true}, standing: Standing{Domain: DomainAccepts}, want: VerdictTypo},
		{name: "a role address is a warning", facts: Facts{Role: true}, standing: Standing{Domain: DomainAccepts}, want: VerdictRole},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Assess(tc.facts, tc.standing, now); got != tc.want {
				t.Fatalf("Assess = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestVerdictBlocksCampaignsOnlyWhenItCannotBeMailed(t *testing.T) {
	blocking := map[Verdict]bool{
		VerdictSuppressed: true, VerdictBadAddress: true, VerdictNoMailDomain: true, VerdictDisposable: true,
		VerdictDomainUnknown: true, VerdictDomainPending: true, VerdictInactive: true,
	}
	for _, v := range AllVerdicts() {
		if got := v.BlocksCampaigns(); got != blocking[v] {
			t.Fatalf("%s.BlocksCampaigns = %v, want %v", v, got, blocking[v])
		}
	}
}

func TestUndeliverableVerdictsAreTheOnesAnImportCanDrop(t *testing.T) {
	want := map[Verdict]bool{VerdictBadAddress: true, VerdictNoMailDomain: true, VerdictDisposable: true}
	for _, v := range AllVerdicts() {
		if got := v.Undeliverable(); got != want[v] {
			t.Fatalf("%s.Undeliverable = %v, want %v", v, got, want[v])
		}
	}
}

func TestCheckerFactsReadsTheInMemorySets(t *testing.T) {
	set, err := ParseDisposableList(listOf(MinDisposableDomains, "mailinator.com"))
	if err != nil {
		t.Fatal(err)
	}
	checker := NewChecker(set, DefaultTypos())
	if f := checker.Facts("ana@mailinator.com"); !f.Disposable {
		t.Fatalf("facts = %+v", f)
	}
	if f := checker.Facts("contato@gmial.com"); f.Suggestion != "gmail.com" || !f.Role {
		t.Fatalf("facts = %+v", f)
	}
	if f := checker.Facts("ana@loja.com.br"); f != (Facts{}) {
		t.Fatalf("facts = %+v", f)
	}
}

func TestSuggestedAddressKeepsTheLocalPart(t *testing.T) {
	got, ok := SuggestedAddress("ana.silva+x@gmial.com", "gmail.com")
	if !ok || got != "ana.silva+x@gmail.com" {
		t.Fatalf("SuggestedAddress = %q %v", got, ok)
	}
	if _, ok := SuggestedAddress("ana@gmial.com", ""); ok {
		t.Fatal("no suggestion, no address")
	}
}
