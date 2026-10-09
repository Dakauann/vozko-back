package shared

import (
	"testing"
	"time"
)

func TestAReservationIsLiveUntilItsDeadlineAndOnlyForItsHolder(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name        string
		reservation Reservation
		live        bool
		heldByA     bool
		takeableByA bool
		takeableByB bool
	}{
		{"nobody holds it", Reservation{}, false, false, true, true},
		{"a holds it before the deadline", Reservation{Holder: "a", Until: now.Add(time.Minute)}, true, true, true, false},
		{"a holds it at the deadline", Reservation{Holder: "a", Until: now}, false, false, true, true},
		{"a held it past the deadline", Reservation{Holder: "a", Until: now.Add(-time.Second)}, false, false, true, true},
		{"b holds it before the deadline", Reservation{Holder: "b", Until: now.Add(time.Minute)}, true, false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.reservation.Live(now); got != tc.live {
				t.Fatalf("Live = %v, want %v", got, tc.live)
			}
			if got := tc.reservation.HeldBy("a", now); got != tc.heldByA {
				t.Fatalf("HeldBy(a) = %v, want %v", got, tc.heldByA)
			}
			if got := tc.reservation.TakeableBy("a", now); got != tc.takeableByA {
				t.Fatalf("TakeableBy(a) = %v, want %v", got, tc.takeableByA)
			}
			if got := tc.reservation.TakeableBy("b", now); got != tc.takeableByB {
				t.Fatalf("TakeableBy(b) = %v, want %v", got, tc.takeableByB)
			}
		})
	}
}

func TestNobodyWithoutANameTakesOrHoldsAReservation(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	if (Reservation{}).TakeableBy("", now) {
		t.Fatal("an empty holder must never take a reservation")
	}
	if (Reservation{Holder: "", Until: now.Add(time.Hour)}).HeldBy("", now) {
		t.Fatal("an empty holder must never hold a reservation")
	}
}

func TestReserveForStartsTheDeadlineFromNow(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	got := ReserveFor("a", now, 5*time.Minute)
	if got.Holder != "a" || !got.Until.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("ReserveFor = %+v", got)
	}
}
