package emailcampaign

import (
	"slices"
	"testing"
)

func TestCampaignsThatMaySendStillKeepTheSenderAccount(t *testing.T) {
	want := map[Status]bool{StatusReady: true, StatusScheduled: true, StatusRunning: true, StatusPaused: true, StatusHeld: true}
	for _, s := range AllStatuses() {
		if got := slices.Contains(AccountKeepingStatuses(), s); got != want[s] {
			t.Errorf("%s keeps the account = %v, want %v", s, got, want[s])
		}
	}
}
