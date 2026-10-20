package channel

import (
	"testing"
	"time"
)

func TestReplyHorizonMatchesTheWindowClock(t *testing.T) {
	day, week := 24*time.Hour, 7*24*time.Hour
	cases := []struct {
		name  string
		caps  Capabilities
		human bool
		want  time.Duration
	}{
		{"no clock", Capabilities{}, true, 0},
		{"standard only", Capabilities{OutboundWindow: day, ExtendedWindow: week}, true, day},
		{"human agent for a person", Capabilities{OutboundWindow: day, ExtendedWindow: week, HumanAgentWindow: true}, true, week},
		{"human agent for automation", Capabilities{OutboundWindow: day, ExtendedWindow: week, HumanAgentWindow: true}, false, day},
		{"extended not longer", Capabilities{OutboundWindow: day, ExtendedWindow: day, HumanAgentWindow: true}, true, day},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.caps.ReplyHorizon(tc.human)
			if got != tc.want {
				t.Fatalf("ReplyHorizon = %v, want %v", got, tc.want)
			}
			if got == 0 {
				return
			}
			now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
			inside, outside := now.Add(-got+time.Minute), now.Add(-got-time.Minute)
			if tier, _ := tc.caps.Window(&inside, now, tc.human); tier == WindowTierNone {
				t.Fatalf("a reply just inside the horizon is refused by Window")
			}
			if tier, _ := tc.caps.Window(&outside, now, tc.human); tier != WindowTierNone {
				t.Fatalf("a reply just past the horizon is allowed by Window")
			}
		})
	}
}
