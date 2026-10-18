package notification

import (
	"testing"
	"time"
)

func TestOnlyNonCriticalEmailsWaitForTheDigestOfPeopleWhoChoseIt(t *testing.T) {
	digest := Preferences{EmailDigest: true}
	if !digest.HoldsForDigest(SeverityWarning) || !digest.HoldsForDigest(SeverityInfo) {
		t.Fatal("non-critical emails wait for the digest")
	}
	if digest.HoldsForDigest(SeverityCritical) {
		t.Fatal("critical emails never wait")
	}
	if (Preferences{}).HoldsForDigest(SeverityInfo) {
		t.Fatal("without the digest every email goes at once")
	}
}

func TestDigestCutoffIsTheLatestEightOClockInBrasilia(t *testing.T) {
	brasilia := time.FixedZone("BRT", -3*60*60)
	cases := []struct {
		now  time.Time
		want time.Time
	}{
		{time.Date(2026, 10, 17, 8, 0, 0, 0, brasilia), time.Date(2026, 10, 17, 8, 0, 0, 0, brasilia)},
		{time.Date(2026, 10, 17, 14, 30, 0, 0, brasilia), time.Date(2026, 10, 17, 8, 0, 0, 0, brasilia)},
		{time.Date(2026, 10, 17, 7, 59, 0, 0, brasilia), time.Date(2026, 10, 16, 8, 0, 0, 0, brasilia)},
		{time.Date(2026, 10, 17, 2, 0, 0, 0, time.UTC), time.Date(2026, 10, 16, 8, 0, 0, 0, brasilia)},
	}
	for _, tc := range cases {
		got := DigestCutoff(tc.now, brasilia)
		if !got.Equal(tc.want) {
			t.Fatalf("DigestCutoff(%v) = %v, want %v", tc.now, got, tc.want)
		}
	}
}

func TestDigestKnowsHowManyItemsDidNotFit(t *testing.T) {
	d := Digest{Items: make([]Item, 3), Total: 8}
	if d.More() != 5 || (Digest{Items: make([]Item, 2), Total: 2}).More() != 0 {
		t.Fatalf("more = %d", d.More())
	}
	if (Digest{Total: 0}).Empty() != true || d.Empty() {
		t.Fatal("a digest with nothing unread is empty")
	}
}
