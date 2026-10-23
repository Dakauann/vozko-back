package invoice_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/invoice"
)

type staleSweepRepo struct {
	stubInvoiceRepo
	cutoff time.Time
	err    error
}

func (r *staleSweepRepo) CancelStaleReservations(createdBefore time.Time) (int64, error) {
	r.cutoff = createdBefore
	return 2, r.err
}

func TestOnlyReservationsOlderThanTheStaleWindowAreCancelled(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	repo := &staleSweepRepo{}
	sweep := NewCancelStaleReservations(repo)
	sweep.now = func() time.Time { return now }

	if err := sweep.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := now.Add(-invoice.ReservationStaleAfter); !repo.cutoff.Equal(want) {
		t.Fatalf("cutoff = %v, want %v; a reservation still being charged must never be cancelled", repo.cutoff, want)
	}
}

func TestAFailedSweepIsReported(t *testing.T) {
	repo := &staleSweepRepo{err: errors.New("db down")}
	if err := NewCancelStaleReservations(repo).Execute(context.Background()); err == nil {
		t.Fatal("the cron runner must see the failure")
	}
}
