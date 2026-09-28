package conversation

import (
	"testing"
	"time"

	"vozko/domain/channel"
)

func TestEvaluateWindow(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	caps := channel.Capabilities{OutboundWindow: 24 * time.Hour, ExtendedWindow: 7 * 24 * time.Hour, HumanAgentWindow: true}
	recent := now.Add(-time.Hour)
	old := now.Add(-30 * time.Hour)
	ancient := now.Add(-8 * 24 * time.Hour)

	cases := []struct {
		name       string
		last       *time.Time
		human      bool
		wantOpen   bool
		wantTier   channel.WindowTier
		wantReason WindowClosedReason
	}{
		{"no inbound", nil, true, false, channel.WindowTierNone, WindowReasonNoInbound},
		{"standard", &recent, false, true, channel.WindowTierStandard, WindowReasonNone},
		{"human tier", &old, true, true, channel.WindowTierHuman, WindowReasonNone},
		{"automation past 24h", &old, false, false, channel.WindowTierNone, WindowReasonExpired},
		{"everyone past 7 days", &ancient, true, false, channel.WindowTierNone, WindowReasonExpired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EvaluateWindow(caps, tc.last, now, tc.human)
			if got.Open != tc.wantOpen || got.Tier != tc.wantTier || got.Reason != tc.wantReason {
				t.Fatalf("window = %+v", got)
			}
		})
	}
}

func TestOpenWindowIsStandardTier(t *testing.T) {
	if OpenWindow(nil).Tier != channel.WindowTierStandard {
		t.Fatal("open window must report the standard tier")
	}
}
