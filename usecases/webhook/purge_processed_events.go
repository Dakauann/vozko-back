package webhook_usecase

import (
	"context"
	"log"
	"time"

	"vozko/domain/webhook"
)

type PurgeProcessedEventsUseCase struct {
	events    webhook.ProcessedEventRepository
	retention time.Duration
}

func NewPurgeProcessedEventsUseCase(events webhook.ProcessedEventRepository, retention time.Duration) *PurgeProcessedEventsUseCase {
	return &PurgeProcessedEventsUseCase{events: events, retention: retention}
}

func (uc *PurgeProcessedEventsUseCase) Execute(ctx context.Context) error {
	purged, err := uc.events.PurgeOlderThan(ctx, time.Now().UTC().Add(-uc.retention))
	if err != nil {
		return err
	}
	if purged > 0 {
		log.Printf("[webhook-dedup] purged %d processed event(s) older than %s", purged, uc.retention)
	}
	return nil
}
