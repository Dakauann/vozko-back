package leadexit

import (
	"time"

	lmw "vozko/domain/lead_message_window"
)

func WindowOpen(receivedAt, now time.Time) bool {
	if receivedAt.IsZero() || now.IsZero() {
		return false
	}
	return now.Sub(receivedAt) < lmw.MessageWindowDuration
}
