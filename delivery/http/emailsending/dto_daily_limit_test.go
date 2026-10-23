package emailsending

import (
	"testing"

	es "vozko/domain/emailsending"
	esuc "vozko/usecases/emailsending"
)

func TestDomainViewCarriesTheDailyLimit(t *testing.T) {
	cases := []struct {
		name    string
		view    esuc.DomainView
		limit   string
		capWant *int64
	}{
		{name: "shared probation domain", view: esuc.DomainView{Domain: es.SendingDomain{Kind: es.DomainSharedProbation}, UsedToday: 3, CapToday: es.ProbationDailyCap}, limit: "capped", capWant: ptr(es.ProbationDailyCap)},
		{name: "shared trusted domain", view: esuc.DomainView{Domain: es.SendingDomain{Kind: es.DomainSharedTrusted}, CapToday: es.TrustedSharedDailyCap}, limit: "capped", capWant: ptr(es.TrustedSharedDailyCap)},
		{name: "verified own domain", view: esuc.DomainView{Domain: es.SendingDomain{Kind: es.DomainOwn, Status: es.DomainVerified}, UsedToday: 80000, CapToday: es.Uncapped}, limit: "unlimited"},
		{name: "unverified own domain", view: esuc.DomainView{Domain: es.SendingDomain{Kind: es.DomainOwn, Status: es.DomainVerifying}, CapToday: 0}, limit: "blocked"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := presentDomainView(tc.view)
			if out.DailyLimit != tc.limit || out.UsedToday == nil || *out.UsedToday != tc.view.UsedToday {
				t.Fatalf("limit %q used %v, want %q and %d", out.DailyLimit, out.UsedToday, tc.limit, tc.view.UsedToday)
			}
			if (out.CapToday == nil) != (tc.capWant == nil) || (out.CapToday != nil && *out.CapToday != *tc.capWant) {
				t.Fatalf("cap_today = %v, want %v", out.CapToday, tc.capWant)
			}
		})
	}
}

func ptr(v int64) *int64 { return &v }
