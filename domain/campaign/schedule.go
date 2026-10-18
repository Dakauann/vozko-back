package campaign

import "time"

type ScheduleClaimer interface {
	ClaimScheduledStart(campaignID string, dueBy time.Time) (bool, error)
}
