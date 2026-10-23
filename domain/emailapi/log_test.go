package emailapi

import (
	"errors"
	"testing"
	"time"
)

func TestCursorRoundTrip(t *testing.T) {
	at := time.Date(2026, 10, 22, 13, 4, 5, 123456000, time.UTC)
	c := Cursor{CreatedAt: at, ID: "0b5f2f3c-6b7d-4e5f-9a0b-1c2d3e4f5a6b"}
	got, err := ParseCursor(c.Encode())
	if err != nil || !got.CreatedAt.Equal(at) || got.ID != c.ID {
		t.Fatalf("round trip = %+v %v", got, err)
	}
	for _, bad := range []string{"%%%", "YWJj", Cursor{CreatedAt: at, ID: "not-a-uuid"}.Encode()} {
		if _, err := ParseCursor(bad); !errors.Is(err, ErrCursorInvalid) {
			t.Fatalf("ParseCursor(%q) = %v", bad, err)
		}
	}
	if got, err := ParseCursor(""); err != nil || !got.Empty() {
		t.Fatalf("empty cursor = %+v %v", got, err)
	}
}

func TestLogQueryNormalizes(t *testing.T) {
	q, err := LogQuery{Limit: 0, Recipient: " Ana@Example.com ", Status: "delivered", TagName: "kind", TagValue: "reset"}.Normalize()
	if err != nil || q.Limit != DefaultPageSize || q.Recipient != "ana@example.com" {
		t.Fatalf("q = %+v %v", q, err)
	}
	if q, _ := (LogQuery{Limit: 1000}).Normalize(); q.Limit != MaxPageSize {
		t.Fatalf("limit = %d", q.Limit)
	}
	from := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	for _, bad := range []LogQuery{
		{Status: "nope"},
		{Recipient: "nope"},
		{TagName: "kind"},
		{TagValue: "x"},
		{TagName: "a b", TagValue: "x"},
		{From: from, To: from.Add(-time.Hour)},
		{Limit: -1},
	} {
		if _, err := bad.Normalize(); !errors.Is(err, ErrFilterInvalid) {
			t.Fatalf("Normalize(%+v) = %v", bad, err)
		}
	}
}

func TestPageNextCursorOnlyWhenThereIsMore(t *testing.T) {
	at := time.Now().UTC()
	rows := []Message{
		{ID: "0b5f2f3c-6b7d-4e5f-9a0b-1c2d3e4f5a6a", CreatedAt: at},
		{ID: "0b5f2f3c-6b7d-4e5f-9a0b-1c2d3e4f5a6b", CreatedAt: at},
		{ID: "0b5f2f3c-6b7d-4e5f-9a0b-1c2d3e4f5a6c", CreatedAt: at},
	}
	page := PageOf(rows, 2)
	if len(page.Items) != 2 || page.Next == "" {
		t.Fatalf("page = %+v", page)
	}
	if next, _ := ParseCursor(page.Next); next.ID != rows[1].ID {
		t.Fatalf("next = %+v", next)
	}
	if last := PageOf(rows[:2], 2); last.Next != "" {
		t.Fatalf("last page has a cursor %q", last.Next)
	}
}
