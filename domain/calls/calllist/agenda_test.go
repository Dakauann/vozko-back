package calllist

import (
	"errors"
	"testing"
	"time"
)

func TestTheAgendaPutsDueCallbacksThenTheQueueThenWaitingCallbacks(t *testing.T) {
	asOf := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	before, after := asOf.Add(-time.Minute), asOf.Add(time.Minute)
	cases := []struct {
		name       string
		callbackAt *time.Time
		want       AgendaSegment
	}{
		{"a callback already due", &before, SegmentDue},
		{"a callback due exactly now", &asOf, SegmentDue},
		{"no callback", nil, SegmentQueue},
		{"a callback still ahead", &after, SegmentWaiting},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AgendaSegmentOf(tc.callbackAt, asOf); got != tc.want {
				t.Fatalf("segment = %v, want %v", got, tc.want)
			}
		})
	}
	if !(SegmentDue < SegmentQueue && SegmentQueue < SegmentWaiting) {
		t.Fatal("the segments must sort due, queue, waiting")
	}
}

func TestAPendingPageNeedsTheInstantItsAgendaWasCutAt(t *testing.T) {
	asOf := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		q    ItemQuery
		want error
	}{
		{"a pending page with its instant", ItemQuery{State: StatePending, AsOf: asOf}, nil},
		{"a later pending page with its instant", ItemQuery{State: StatePending, AsOf: asOf, AfterPosition: 9}, nil},
		{"a pending page without its instant", ItemQuery{State: StatePending}, ErrItemCursorInvalid},
		{"a later pending page without its instant", ItemQuery{State: StatePending, AfterPosition: 9}, ErrItemCursorInvalid},
		{"a closed page", ItemQuery{State: StateClosed, AfterPosition: 9}, nil},
		{"every state", ItemQuery{AfterPosition: 9}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.q.Check(); !errors.Is(err, tc.want) {
				t.Fatalf("Check = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestAPendingCursorTellsWhereTheNextPageStarts(t *testing.T) {
	asOf := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	due, ahead := asOf.Add(-time.Hour), asOf.Add(time.Hour)
	cases := []struct {
		name      string
		q         ItemQuery
		continues bool
		segment   AgendaSegment
	}{
		{"the first page", ItemQuery{AsOf: asOf}, false, SegmentDue},
		{"after a due callback", ItemQuery{AsOf: asOf, AfterPosition: 4, AfterAt: &due}, true, SegmentDue},
		{"after a queued item", ItemQuery{AsOf: asOf, AfterPosition: 4}, true, SegmentQueue},
		{"after a waiting callback", ItemQuery{AsOf: asOf, AfterPosition: 4, AfterAt: &ahead}, true, SegmentWaiting},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.q.Continues(); got != tc.continues {
				t.Fatalf("Continues = %v, want %v", got, tc.continues)
			}
			if got := tc.q.CursorSegment(); got != tc.segment {
				t.Fatalf("CursorSegment = %v, want %v", got, tc.segment)
			}
		})
	}
}

func TestNormalizingAnItemQueryKeepsItsInstantsInUTC(t *testing.T) {
	zone := time.FixedZone("BRT", -3*60*60)
	asOf := time.Date(2026, 10, 8, 9, 0, 0, 0, zone)
	afterAt := asOf.Add(time.Hour)
	q := ItemQuery{AsOf: asOf, AfterAt: &afterAt}.Normalized()
	if q.AsOf.Location() != time.UTC || !q.AsOf.Equal(asOf) || q.AfterAt.Location() != time.UTC || !q.AfterAt.Equal(afterAt) {
		t.Fatalf("normalized = %v, %v", q.AsOf, q.AfterAt)
	}
	if afterAt.Location() != zone {
		t.Fatal("normalizing must not change the caller's time")
	}
}
