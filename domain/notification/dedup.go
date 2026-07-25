package notification

import "time"

// Dedup guarantees a notification is delivered at most once per logical event,
// surviving cron rescans (reminders), webhook redeliveries, and consumer retries.
//
// Key construction by category:
//   - reminders (cron): "{type}:{entityID}:{periodAnchor}" where periodAnchor is
//     CurrentPeriodEnd. The anchor changes when the plan/addon renews, which
//     re-arms the reminder for the next cycle.
//   - state-change events (webhooks): "{type}:{entityID}:{newState}".
//   - recurring conditions (low/negative balance): "{type}:{workspace}:{band}",
//     cleared with Clear when the condition recovers so it can fire again.
type Dedup interface {
	// FirstTime returns true exactly once for key within ttl (then false until ttl
	// lapses). On a backend error it returns (false, err) so the caller suppresses
	// the send rather than risk spamming; the caller should log the error.
	FirstTime(key string, ttl time.Duration) (bool, error)
	// Clear re-arms a key so its next occurrence notifies again.
	Clear(key string) error
}
