package whatsapp_campaign_entry

import (
	"testing"

	wce "vozko/domain/whatsapp_campaign_entry"
)

// addWAStatusCount is the shared mapping used by both CountByStatus and the
// batched CountByStatusForCampaigns, so its correctness underpins the metrics
// summary. It must accumulate (not overwrite), bucket each status correctly,
// count unknown statuses toward Total only, and keep Total in sync.
func TestAddWAStatusCount(t *testing.T) {
	counts := &wce.StatusCounts{}

	addWAStatusCount(counts, string(wce.SendStatusSent), 3)
	addWAStatusCount(counts, string(wce.SendStatusSent), 2) // accumulates with the previous SENT
	addWAStatusCount(counts, string(wce.SendStatusDelivered), 4)
	addWAStatusCount(counts, string(wce.SendStatusRead), 1)
	addWAStatusCount(counts, string(wce.SendStatusFailed), 5)
	addWAStatusCount(counts, string(wce.SendStatusNotEligiblePossibleSpam), 6)
	addWAStatusCount(counts, string(wce.SendStatusPending), 7)
	addWAStatusCount(counts, "SOMETHING_UNKNOWN", 8) // no bucket → Total only

	cases := map[string]struct{ got, want int64 }{
		"Sent":      {counts.Sent, 5},
		"Delivered": {counts.Delivered, 4},
		"Read":      {counts.Read, 1},
		"Failed":    {counts.Failed, 5},
		"Spam":      {counts.NotEligiblePossibleSpam, 6},
		"Pending":   {counts.Pending, 7},
		"Total":     {counts.Total, 36}, // 3+2+4+1+5+6+7+8
	}
	for name, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", name, c.got, c.want)
		}
	}

	// Billed dispatches = Total - Pending - Failed - Spam = 36 - 7 - 5 - 6 = 18.
	// Note the unknown-status row (8) counts as billed: that's the subtractive
	// definition working, not SENT+DELIVERED+READ (which would be 10).
	if got, want := counts.Dispatches(), int64(18); got != want {
		t.Errorf("Dispatches() = %d, want %d", got, want)
	}
}

func TestDedupeNonEmpty(t *testing.T) {
	got := dedupeNonEmpty([]string{" a ", "a", "", "  ", "b", "a"})
	want := []string{"a", "b"}
	if len(got) != len(want) {
		t.Fatalf("dedupeNonEmpty len = %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("dedupeNonEmpty[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	if dedupeNonEmpty(nil) != nil {
		t.Error("dedupeNonEmpty(nil) should be nil")
	}
	if got := dedupeNonEmpty([]string{"", "   "}); got != nil && len(got) != 0 {
		t.Errorf("dedupeNonEmpty of only-blanks should be empty, got %v", got)
	}
}
