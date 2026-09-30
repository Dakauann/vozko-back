package callrouting_infra

import (
	"testing"
	"time"
)

func TestActivityIsKeptPerWorkspaceAndReturnedInTheAskedOrder(t *testing.T) {
	activity := NewAgentActivity()
	ended := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	activity.CallAnswered("ws1", "ana")
	activity.CallAnswered("ws1", "ana")
	activity.CallEnded("ws1", "ana", ended)
	activity.CallAnswered("ws2", "ana")

	stats := activity.Stats("ws1", []string{"bia", "ana"})
	if len(stats) != 2 || stats[0].UserID != "bia" || stats[1].UserID != "ana" {
		t.Fatalf("stats = %+v", stats)
	}
	if stats[1].CallsAnswered != 2 || !stats[1].LastCallEndedAt.Equal(ended) {
		t.Fatalf("ana = %+v", stats[1])
	}
	if !stats[0].LastCallEndedAt.IsZero() || stats[0].CallsAnswered != 0 {
		t.Fatalf("an agent without calls should look fresh: %+v", stats[0])
	}
	if other := activity.Stats("ws2", []string{"ana"}); other[0].CallsAnswered != 1 {
		t.Fatalf("workspaces mixed: %+v", other)
	}
}
