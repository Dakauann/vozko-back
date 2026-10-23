package emailhygiene

import (
	"testing"
	"time"
)

func TestClassifyFollowsRFC5321AndRFC7505(t *testing.T) {
	cases := []struct {
		name   string
		answer DNSAnswer
		state  DomainState
		detail DomainDetail
	}{
		{name: "mx records accept mail", answer: DNSAnswer{MX: LookupFound, MXHosts: []string{"mx1.loja.com.br"}}, state: DomainAccepts, detail: DetailMX},
		{name: "a single null mx refuses mail", answer: DNSAnswer{MX: LookupFound, MXHosts: []string{"."}}, state: DomainNoMail, detail: DetailNullMX},
		{name: "a null mx written without the dot still refuses mail", answer: DNSAnswer{MX: LookupFound, MXHosts: []string{""}}, state: DomainNoMail, detail: DetailNullMX},
		{name: "a null mx next to a real host is ignored because the real host is usable", answer: DNSAnswer{MX: LookupFound, MXHosts: []string{".", "mx.loja.com.br"}}, state: DomainAccepts, detail: DetailMX},
		{name: "no mx but an address is the implicit mx", answer: DNSAnswer{MX: LookupAbsent, Address: LookupFound}, state: DomainAccepts, detail: DetailImplicitMX},
		{name: "no mx and no address once is unknown until a later check repeats it", answer: DNSAnswer{MX: LookupAbsent, Address: LookupAbsent}, state: DomainUnknown, detail: DetailNoRecords},
		{name: "an mx timeout is unknown never invalid", answer: DNSAnswer{MX: LookupTimedOut}, state: DomainUnknown, detail: DetailTimeout},
		{name: "an mx failure is unknown never invalid", answer: DNSAnswer{MX: LookupFailed}, state: DomainUnknown, detail: DetailError},
		{name: "an address timeout after no mx is unknown", answer: DNSAnswer{MX: LookupAbsent, Address: LookupTimedOut}, state: DomainUnknown, detail: DetailTimeout},
		{name: "an address failure after no mx is unknown", answer: DNSAnswer{MX: LookupAbsent, Address: LookupFailed}, state: DomainUnknown, detail: DetailError},
		{name: "an answer never looked up is unknown, never silently valid", answer: DNSAnswer{}, state: DomainUnknown, detail: DetailError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.answer)
			if got.State != tc.state || got.Detail != tc.detail {
				t.Fatalf("Classify(%+v) = %+v, want %s/%s", tc.answer, got, tc.state, tc.detail)
			}
		})
	}
}

func TestSettleKeepsVerdictsForTheirTTLAndRetriesUnknown(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		state    DomainState
		attempts int
		expires  time.Duration
		due      time.Duration
		hasDue   bool
	}{
		{name: "accepts is kept a week", state: DomainAccepts, attempts: 1, expires: 7 * 24 * time.Hour},
		{name: "no mail is kept three days", state: DomainNoMail, attempts: 1, expires: 3 * 24 * time.Hour},
		{name: "first unknown retries after thirty minutes", state: DomainUnknown, attempts: 1, due: 30 * time.Minute, hasDue: true},
		{name: "second unknown retries after two hours", state: DomainUnknown, attempts: 2, due: 2 * time.Hour, hasDue: true},
		{name: "third unknown retries after six hours", state: DomainUnknown, attempts: 3, due: 6 * time.Hour, hasDue: true},
		{name: "later unknowns retry daily", state: DomainUnknown, attempts: 9, due: 24 * time.Hour, hasDue: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := Settle(DomainVerdict{State: tc.state}, tc.attempts, now)
			if tc.hasDue {
				if s.DueAt == nil || !s.DueAt.Equal(now.Add(tc.due)) {
					t.Fatalf("due = %v, want %v", s.DueAt, now.Add(tc.due))
				}
				if !s.ExpiresAt.Equal(now) {
					t.Fatalf("an unknown verdict must be stale at once, expires %v", s.ExpiresAt)
				}
				return
			}
			if s.DueAt != nil {
				t.Fatalf("a settled verdict must leave the queue, due %v", s.DueAt)
			}
			if !s.ExpiresAt.Equal(now.Add(tc.expires)) {
				t.Fatalf("expires = %v, want %v", s.ExpiresAt, now.Add(tc.expires))
			}
		})
	}
}

func TestCheckStaleness(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	fresh := DomainCheck{Domain: "loja.com.br", State: DomainAccepts, ExpiresAt: now.Add(time.Hour)}
	stale := DomainCheck{Domain: "loja.com.br", State: DomainAccepts, ExpiresAt: now.Add(-time.Second)}
	pending := DomainCheck{Domain: "loja.com.br", State: DomainPending}
	if fresh.Stale(now) {
		t.Fatal("a check before its expiry is fresh")
	}
	if !stale.Stale(now) {
		t.Fatal("a check past its expiry is stale")
	}
	if !pending.Stale(now) {
		t.Fatal("a pending check has no verdict yet and is always due")
	}
}

func TestCanaryHealthyOnlyWhenItAcceptsMail(t *testing.T) {
	if !CanaryHealthy(DomainVerdict{State: DomainAccepts}) {
		t.Fatal("an accepting canary means the resolver works")
	}
	for _, s := range []DomainState{DomainNoMail, DomainUnknown, DomainPending} {
		if CanaryHealthy(DomainVerdict{State: s}) {
			t.Fatalf("a canary that is %s means our resolver is not trustworthy", s)
		}
	}
}

func TestConfirmOnlyTurnsARepeatedEmptyAnswerIntoNoMail(t *testing.T) {
	empty := Classify(DNSAnswer{MX: LookupAbsent, Address: LookupAbsent})
	cases := []struct {
		name  string
		v     DomainVerdict
		prior DomainCheck
		want  DomainVerdict
	}{
		{name: "a first empty answer stays unknown", v: empty, prior: DomainCheck{State: DomainPending}, want: DomainVerdict{State: DomainUnknown, Detail: DetailNoRecords}},
		{name: "an empty answer after a timeout stays unknown", v: empty, prior: DomainCheck{State: DomainUnknown, Detail: DetailTimeout}, want: DomainVerdict{State: DomainUnknown, Detail: DetailNoRecords}},
		{name: "an empty answer after an accepting check stays unknown", v: empty, prior: DomainCheck{State: DomainAccepts, Detail: DetailMX}, want: DomainVerdict{State: DomainUnknown, Detail: DetailNoRecords}},
		{name: "a second empty answer in a row refuses mail", v: empty, prior: DomainCheck{State: DomainUnknown, Detail: DetailNoRecords}, want: DomainVerdict{State: DomainNoMail, Detail: DetailNoRecordsConfirmed}},
		{name: "an empty answer after a confirmed one stays refused", v: empty, prior: DomainCheck{State: DomainNoMail, Detail: DetailNoRecordsConfirmed}, want: DomainVerdict{State: DomainNoMail, Detail: DetailNoRecordsConfirmed}},
		{name: "an empty answer after a legacy unconfirmed refusal stays unknown", v: empty, prior: DomainCheck{State: DomainNoMail, Detail: DetailNoRecords}, want: DomainVerdict{State: DomainUnknown, Detail: DetailNoRecords}},
		{name: "a null mx is definite at once", v: DomainVerdict{State: DomainNoMail, Detail: DetailNullMX}, prior: DomainCheck{State: DomainPending}, want: DomainVerdict{State: DomainNoMail, Detail: DetailNullMX}},
		{name: "an accepting answer always wins", v: DomainVerdict{State: DomainAccepts, Detail: DetailMX}, prior: DomainCheck{State: DomainUnknown, Detail: DetailNoRecords}, want: DomainVerdict{State: DomainAccepts, Detail: DetailMX}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Confirm(tc.v, tc.prior); got != tc.want {
				t.Fatalf("Confirm = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestOnlyDefiniteDetailsMayRefuseMail(t *testing.T) {
	for _, detail := range []DomainDetail{DetailNullMX, DetailNoRecordsConfirmed} {
		if !DefiniteNoMail(detail) {
			t.Fatalf("%q is a definite answer", detail)
		}
	}
	for _, detail := range []DomainDetail{DetailNone, DetailMX, DetailImplicitMX, DetailNoRecords, DetailTimeout, DetailError} {
		if DefiniteNoMail(detail) {
			t.Fatalf("%q must never refuse mail", detail)
		}
	}
}
