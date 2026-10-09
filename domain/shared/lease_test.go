package shared

import (
	"testing"
	"time"
)

func TestLeaseIsFreeWhenUnclaimedOrItsHeartbeatIsStale(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	fresh, stale := now.Add(-time.Minute), now.Add(-3*time.Minute)
	cases := []struct {
		name  string
		lease Lease
		want  bool
	}{
		{"never claimed", Lease{}, true},
		{"claimed without a heartbeat", Lease{Claim: "w1"}, true},
		{"claimed with a stale heartbeat", Lease{Claim: "w1", HeartbeatAt: &stale}, true},
		{"claimed and alive", Lease{Claim: "w1", HeartbeatAt: &fresh}, false},
		{"out of attempts", Lease{Attempts: 3}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.lease.Claimable(now, 2*time.Minute, 3); got != tc.want {
				t.Fatalf("Claimable = %v, want %v", got, tc.want)
			}
		})
	}
}
