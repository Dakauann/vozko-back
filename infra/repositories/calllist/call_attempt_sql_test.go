package calllist_repository

import "testing"

func TestACallAttemptIsAnOutboundCallThatWasNotDeleted(t *testing.T) {
	if got, want := callAttemptSQL("ca"), "ca.direction = 'outbound' AND ca.deleted_at IS NULL"; got != want {
		t.Fatalf("callAttemptSQL = %q, want %q", got, want)
	}
}
