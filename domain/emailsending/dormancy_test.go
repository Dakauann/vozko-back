package emailsending

import (
	"testing"
	"time"
)

func TestDormant(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name         string
		lastActiveAt time.Time
		liveCampaign bool
		want         bool
	}{
		{name: "ninety days without sending retires", lastActiveAt: now.Add(-DormancyAfter), want: true},
		{name: "longer idle retires", lastActiveAt: now.AddDate(0, -6, 0), want: true},
		{name: "one minute short of ninety days stays", lastActiveAt: now.Add(-DormancyAfter + time.Minute)},
		{name: "a recent send stays", lastActiveAt: now.AddDate(0, 0, -3)},
		{name: "a live campaign keeps an idle account", lastActiveAt: now.AddDate(-1, 0, 0), liveCampaign: true},
		{name: "an unknown last activity never retires", lastActiveAt: time.Time{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Dormant(tc.lastActiveAt, now, tc.liveCampaign); got != tc.want {
				t.Fatalf("Dormant = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDormancyAfterIsNinetyDays(t *testing.T) {
	if DormancyAfter != 90*24*time.Hour {
		t.Fatalf("DormancyAfter = %v", DormancyAfter)
	}
}

func TestLastActiveAt(t *testing.T) {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	updated := created.AddDate(0, 2, 0)
	sent := created.AddDate(0, 5, 0)
	early := created.AddDate(0, 1, 0)
	cases := []struct {
		name    string
		account SenderAccount
		sent    *time.Time
		want    time.Time
	}{
		{name: "never sent counts from creation", account: SenderAccount{CreatedAt: created}, want: created},
		{name: "an update after creation counts", account: SenderAccount{CreatedAt: created, UpdatedAt: updated}, want: updated},
		{name: "the last send counts when latest", account: SenderAccount{CreatedAt: created, UpdatedAt: updated}, sent: &sent, want: sent},
		{name: "an older send does not move it back", account: SenderAccount{CreatedAt: created, UpdatedAt: updated}, sent: &early, want: updated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.account.LastActiveAt(tc.sent); !got.Equal(tc.want) {
				t.Fatalf("LastActiveAt = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHasProviderIdentity(t *testing.T) {
	cases := []struct {
		name   string
		domain SendingDomain
		want   bool
	}{
		{name: "pending setup has no identity yet", domain: SendingDomain{Status: DomainPending}},
		{name: "verifying", domain: SendingDomain{Status: DomainVerifying}, want: true},
		{name: "verified", domain: SendingDomain{Status: DomainVerified}, want: true},
		{name: "failed verification keeps the identity", domain: SendingDomain{Status: DomainFailed}, want: true},
		{name: "retired", domain: SendingDomain{Status: DomainVerified, Retired: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.domain.HasProviderIdentity(); got != tc.want {
				t.Fatalf("HasProviderIdentity = %v, want %v", got, tc.want)
			}
		})
	}
}
