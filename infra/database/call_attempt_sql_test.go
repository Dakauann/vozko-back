package database

import "testing"

func TestACallAttemptIsAnOutboundCallThatWasNotDeleted(t *testing.T) {
	if got, want := CallAttemptSQL("ca"), "ca.direction = 'outbound' AND ca.deleted_at IS NULL"; got != want {
		t.Fatalf("CallAttemptSQL = %q, want %q", got, want)
	}
}
