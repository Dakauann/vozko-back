package notification

import (
	"errors"
	"testing"
	"time"
)

func TestRecipientCursorRoundTripsAndRefusesForeignValues(t *testing.T) {
	c := RecipientCursor{NotificationID: "n-1", UserID: "u-9"}
	parsed, paged, err := ParseRecipientCursor(c.Encode())
	if err != nil || !paged || parsed != c {
		t.Fatalf("parsed %+v paged %v err %v", parsed, paged, err)
	}
	if _, paged, err := ParseRecipientCursor(""); err != nil || paged {
		t.Fatal("an empty cursor means the first page")
	}
	for _, bad := range []string{"%%%", RecipientCursor{NotificationID: "n-1"}.Encode()} {
		if _, _, err := ParseRecipientCursor(bad); !errors.Is(err, ErrCursorInvalid) {
			t.Fatalf("%q = %v", bad, err)
		}
	}
}

func TestReadStatusFilterParses(t *testing.T) {
	for raw, want := range map[string]ReadStatus{"": ReadStatusAll, "all": ReadStatusAll, "read": ReadStatusRead, "unread": ReadStatusUnread} {
		got, err := ParseReadStatus(raw)
		if err != nil || got != want {
			t.Fatalf("%q = %v, %v", raw, got, err)
		}
	}
	if _, err := ParseReadStatus("seen"); !errors.Is(err, ErrReadStatusUnknown) {
		t.Fatalf("unknown = %v", err)
	}
}

func TestRecipientPageTrimsTheProbeRow(t *testing.T) {
	at := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	rows := []BroadcastRecipient{{NotificationID: "n-1", UserID: "a"}, {NotificationID: "n-1", UserID: "b", ReadAt: &at}, {NotificationID: "n-2", UserID: "a"}}
	page := RecipientPageOf(rows, 2)
	if len(page.Items) != 2 || page.Next == "" {
		t.Fatalf("page = %+v", page)
	}
	next, _, _ := ParseRecipientCursor(page.Next)
	if next != (RecipientCursor{NotificationID: "n-1", UserID: "b"}) {
		t.Fatalf("next = %+v", next)
	}
	if last := RecipientPageOf(rows, 3); last.Next != "" {
		t.Fatal("no probe row, no next page")
	}
}

func TestBroadcastStatsReadRate(t *testing.T) {
	if (BroadcastStats{Recipients: 4, Read: 1}).ReadRate() != 0.25 || (BroadcastStats{}).ReadRate() != 0 {
		t.Fatal("read rate is read over recipients, zero when nobody received it")
	}
}
