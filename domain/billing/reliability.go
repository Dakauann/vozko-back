package billing

import "time"

// ProcessedEvent is a durable record that a provider webhook event has been handled, so a
// redelivery (or a Redis flush) cannot trigger a second money-affecting side effect. The unique
// key is (Provider, EventID). Postgres is the durable guard; the existing Redis IdempotencyGuard
// stays as a fast first check. Billing is the first consumer, but the store is general-purpose:
// any provider webhook can dedup through it.
type ProcessedEvent struct {
	ID          string    `json:"id"`
	Provider    string    `json:"provider"`
	EventID     string    `json:"eventId"`
	ProcessedAt time.Time `json:"processedAt"`
}

// ProcessedEventRepository is the durable webhook-dedup store.
type ProcessedEventRepository interface {
	// MarkProcessed atomically records (provider, eventID). It returns firstTime=true when this call
	// was the first to record the pair (the caller should perform the side effect) and false when the
	// pair already existed (a duplicate; the caller must skip). Implemented as an idempotent
	// INSERT ... ON CONFLICT DO NOTHING, so under concurrency exactly one caller observes firstTime=true.
	MarkProcessed(provider, eventID string, at time.Time) (firstTime bool, err error)
}
