package billing

import "time"

type ProcessedEvent struct {
	ID          string    `json:"id"`
	Provider    string    `json:"provider"`
	EventID     string    `json:"eventId"`
	ProcessedAt time.Time `json:"processedAt"`
}

type ProcessedEventRepository interface {
	MarkProcessed(provider, eventID string, at time.Time) (firstTime bool, err error)
}
