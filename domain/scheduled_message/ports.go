package scheduled_message

import (
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type WindowReader interface {
	GetWindowStatusForEntry(entryID, entryType string) conversation.WindowState
}

type WakeScheduler interface {
	ScheduleFire(id string, fireAt time.Time) error
}

type Clock = shared.Clock

type SystemClock = shared.SystemClock
