package conversation

import (
	"testing"
	"time"

	"vozko/domain/channel"
)

func TestWindowStateEqual(t *testing.T) {
	at := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	sameInstant := at.In(time.FixedZone("BRT", -3*3600))
	later := at.Add(time.Minute)

	cases := []struct {
		name string
		a, b WindowState
		want bool
	}{
		{"same open deadline in another zone", OpenWindow(&at), OpenWindow(&sameInstant), true},
		{"different deadline", OpenWindow(&at), OpenWindow(&later), false},
		{"deadline against none", OpenWindow(&at), OpenWindow(nil), false},
		{"both closed for the same reason", ClosedWindow(WindowReasonExpired), ClosedWindow(WindowReasonExpired), true},
		{"closed for different reasons", ClosedWindow(WindowReasonExpired), ClosedWindow(WindowReasonNoInbound), false},
		{"provider detail differs", ClosedByProvider("blocked_by_buyer"), ClosedByProvider("blocked_by_mediation"), false},
		{"tier differs", WindowState{Open: true, Tier: channel.WindowTierStandard}, WindowState{Open: true, Tier: channel.WindowTierHuman}, false},
		{"open against closed", OpenWindow(nil), ClosedWindow(WindowReasonChannelUnavailable), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.Equal(tc.b); got != tc.want {
				t.Fatalf("Equal = %v, want %v", got, tc.want)
			}
			if got := tc.b.Equal(tc.a); got != tc.want {
				t.Fatalf("Equal is not symmetric")
			}
		})
	}
}

func TestUnavailableWindowsFillsEveryMissingEntry(t *testing.T) {
	at := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	known := map[string]WindowState{"a": OpenWindow(&at)}

	got := WithUnavailable([]string{"a", "b", "b", ""}, known)

	if len(got) != 2 {
		t.Fatalf("got %d states, want 2: %+v", len(got), got)
	}
	if !got["a"].Equal(OpenWindow(&at)) {
		t.Errorf("a = %+v, want the known state", got["a"])
	}
	if got["b"].Open || got["b"].Reason != WindowReasonChannelUnavailable {
		t.Errorf("b = %+v, want closed channel_unavailable", got["b"])
	}
}
