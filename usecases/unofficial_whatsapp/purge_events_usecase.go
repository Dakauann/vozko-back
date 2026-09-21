package unofficial_whatsapp

import (
	"context"
	"log"
	"time"

	uw "vozko/domain/unofficial_whatsapp"
)

type PurgeProcessedEventsUseCase struct {
	events    uw.ProcessedEventRepository
	retention time.Duration
}

func NewPurgeProcessedEventsUseCase(events uw.ProcessedEventRepository, retention time.Duration) *PurgeProcessedEventsUseCase {
	if retention <= 0 {
		retention = 30 * 24 * time.Hour
	}
	return &PurgeProcessedEventsUseCase{events: events, retention: retention}
}

func (uc *PurgeProcessedEventsUseCase) Execute(ctx context.Context) error {
	cutoff := time.Now().UTC().Add(-uc.retention)
	removed, err := uc.events.PurgeOlderThan(ctx, cutoff)
	if err != nil {
		return err
	}
	if removed > 0 {
		log.Printf("[unofficial-whatsapp] purged %d processed webhook event(s) older than %s",
			removed, uc.retention)
	}
	return nil
}
