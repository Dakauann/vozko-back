package emailinbox

import (
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/emailsending"
)

func TestWindowOf(t *testing.T) {
	active := &emailsending.SenderAccount{TenantName: "ws-1", Mode: emailsending.ModeProbation, Status: emailsending.AccountActive}
	paused := &emailsending.SenderAccount{TenantName: "ws-1", Mode: emailsending.ModeProbation, Status: emailsending.AccountPausedByProvider}
	unprovisioned := &emailsending.SenderAccount{Status: emailsending.AccountActive}
	dormant := &emailsending.SenderAccount{TenantName: "ws-1", Mode: emailsending.ModeProbation, Status: emailsending.AccountDormant}

	cases := []struct {
		name       string
		account    *emailsending.SenderAccount
		suppressed bool
		open       bool
		detail     string
	}{
		{"active and reachable", active, false, true, ""},
		{"dormant reopens on the next reply", dormant, false, true, ""},
		{"no account", nil, false, false, ProviderDetailSenderNotProvisioned},
		{"account without tenant", unprovisioned, false, false, ProviderDetailSenderNotProvisioned},
		{"paused", paused, false, false, ProviderDetailSenderPaused},
		{"suppressed address", active, true, false, ProviderDetailAddressSuppressed},
		{"paused wins over suppressed", paused, true, false, ProviderDetailSenderPaused},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := WindowOf(tc.account, tc.suppressed)
			if got.Open != tc.open || got.ProviderDetail != tc.detail {
				t.Fatalf("WindowOf = %+v, want open=%v detail=%q", got, tc.open, tc.detail)
			}
			if !tc.open && got.Reason != conversation.WindowReasonProviderBlocked {
				t.Fatalf("closed reason = %q", got.Reason)
			}
		})
	}
}
