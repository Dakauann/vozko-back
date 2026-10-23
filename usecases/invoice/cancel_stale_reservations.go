package invoice_usecase

import (
	"context"
	"fmt"
	"log"
	"time"

	"vozko/domain/invoice"
)

type CancelStaleReservations struct {
	repo invoice.Repository
	now  func() time.Time
}

func NewCancelStaleReservations(repo invoice.Repository) *CancelStaleReservations {
	return &CancelStaleReservations{repo: repo, now: time.Now}
}

func (uc *CancelStaleReservations) Execute(context.Context) error {
	cancelled, err := uc.repo.CancelStaleReservations(uc.now().Add(-invoice.ReservationStaleAfter))
	if err != nil {
		return fmt.Errorf("cancel stale invoice reservations: %w", err)
	}
	if cancelled > 0 {
		log.Printf("[invoice] cancelled %d reservation(s) whose charge was never attached; a late payment still finds them by reference", cancelled)
	}
	return nil
}
