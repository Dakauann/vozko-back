package emailsending

import (
	"testing"
	"time"
)

func TestDayKeyUsesBrasiliaTime(t *testing.T) {
	cases := []struct {
		at   time.Time
		want string
	}{
		{at: time.Date(2026, 10, 21, 2, 59, 0, 0, time.UTC), want: "2026-10-20"},
		{at: time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC), want: "2026-10-21"},
	}
	for _, tc := range cases {
		if got := DayKey(tc.at); got != tc.want {
			t.Fatalf("DayKey(%v) = %q, want %q", tc.at, got, tc.want)
		}
	}
}

func TestNextDayStart(t *testing.T) {
	at := time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC)
	want := time.Date(2026, 10, 22, 3, 0, 0, 0, time.UTC)
	if got := NextDayStart(at); !got.Equal(want) {
		t.Fatalf("NextDayStart = %v, want %v", got, want)
	}
}

func TestDailyCap(t *testing.T) {
	cases := []struct {
		name   string
		domain SendingDomain
		want   int64
	}{
		{name: "probation shared domain", domain: SendingDomain{Kind: DomainSharedProbation}, want: 500},
		{name: "verified probation shared domain", domain: SendingDomain{Kind: DomainSharedProbation, Status: DomainVerified}, want: 500},
		{name: "trusted shared domain", domain: SendingDomain{Kind: DomainSharedTrusted}, want: 5000},
		{name: "verified trusted shared domain", domain: SendingDomain{Kind: DomainSharedTrusted, Status: DomainVerified}, want: 5000},
		{name: "own domain is uncapped from verification", domain: SendingDomain{Kind: DomainOwn, Status: DomainVerified}, want: Uncapped},
		{name: "pending own domain cannot send", domain: SendingDomain{Kind: DomainOwn, Status: DomainPending}, want: 0},
		{name: "verifying own domain cannot send", domain: SendingDomain{Kind: DomainOwn, Status: DomainVerifying}, want: 0},
		{name: "failed own domain cannot send", domain: SendingDomain{Kind: DomainOwn, Status: DomainFailed}, want: 0},
		{name: "retired own domain cannot send", domain: SendingDomain{Kind: DomainOwn, Status: DomainVerified, Retired: true}, want: 0},
		{name: "unknown kind cannot send", domain: SendingDomain{Kind: "other", Status: DomainVerified}, want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DailyCap(tc.domain); got != tc.want {
				t.Fatalf("DailyCap = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestDailyLimitOf(t *testing.T) {
	cases := map[int64]DailyLimit{
		ProbationDailyCap:     DailyLimitCapped,
		TrustedSharedDailyCap: DailyLimitCapped,
		Uncapped:              DailyLimitUnlimited,
		0:                     DailyLimitBlocked,
		-7:                    DailyLimitBlocked,
	}
	for capValue, want := range cases {
		if got := DailyLimitOf(capValue); got != want {
			t.Fatalf("DailyLimitOf(%d) = %q, want %q", capValue, got, want)
		}
	}
}
