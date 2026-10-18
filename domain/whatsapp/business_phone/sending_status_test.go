package businessphone

import "testing"

func TestOnlyStatusesMetaRefusesToSendFromBlockSending(t *testing.T) {
	blocking := []Status{StatusBanned, StatusRestricted, StatusDisconnected, StatusSuspended, StatusRateLimited, StatusOnboardingFailed, StatusPending, StatusVerifying}
	for _, status := range blocking {
		if !status.BlocksSending() {
			t.Fatalf("%s must block sending", status)
		}
	}
	for _, status := range []Status{StatusConnected, StatusFlagged, ""} {
		if status.BlocksSending() {
			t.Fatalf("%q must not block sending", status)
		}
	}
}
