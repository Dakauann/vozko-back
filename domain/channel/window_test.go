package channel

import (
	"testing"
	"time"
)

func TestWindowTiers(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) *time.Time { v := now.Add(-d); return &v }

	messenger := Capabilities{OutboundWindow: 24 * time.Hour, ExtendedWindow: 7 * 24 * time.Hour, HumanAgentWindow: true}
	noApproval := messenger
	noApproval.HumanAgentWindow = false
	unlimited := Capabilities{}

	cases := []struct {
		name        string
		caps        Capabilities
		lastInbound *time.Time
		human       bool
		wantTier    WindowTier
		wantExpiry  *time.Time
	}{
		{"never messaged", messenger, nil, true, WindowTierNone, nil},
		{"inside 24h for automation", messenger, ago(time.Hour), false, WindowTierStandard, ago(-23 * time.Hour)},
		{"inside 24h for a human", messenger, ago(time.Hour), true, WindowTierStandard, ago(-23 * time.Hour)},
		{"24h boundary closes standard", messenger, ago(24 * time.Hour), false, WindowTierNone, nil},
		{"25h human with approval", messenger, ago(25 * time.Hour), true, WindowTierHuman, ago(25*time.Hour - 7*24*time.Hour)},
		{"25h automation never gets the human tier", messenger, ago(25 * time.Hour), false, WindowTierNone, nil},
		{"25h human without approval", noApproval, ago(25 * time.Hour), true, WindowTierNone, nil},
		{"past 7 days", messenger, ago(7*24*time.Hour + time.Minute), true, WindowTierNone, nil},
		{"channel without a window", unlimited, nil, false, WindowTierStandard, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tier, expiry := tc.caps.Window(tc.lastInbound, now, tc.human)
			if tier != tc.wantTier {
				t.Fatalf("tier = %q, want %q", tier, tc.wantTier)
			}
			if (expiry == nil) != (tc.wantExpiry == nil) || (expiry != nil && !expiry.Equal(*tc.wantExpiry)) {
				t.Fatalf("expiry = %v, want %v", expiry, tc.wantExpiry)
			}
		})
	}
}
