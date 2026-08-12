package scheduled_message_usecase

import (
	"context"
	"fmt"
	"log"
	"time"

	sm "vozko/domain/scheduled_message"
)

// terminalRetention is how long a delivered, failed or cancelled message stays
// readable. Long enough to answer "what did we send that customer in March",
// short enough that the table does not grow without bound.
const terminalRetention = 90 * 24 * time.Hour

type purgeJob struct {
	repo  sm.Repository
	clock sm.Clock
}

func NewPurgeJob(repo sm.Repository, clock sm.Clock) (sm.PurgeJob, error) {
	if repo == nil || clock == nil {
		return nil, fmt.Errorf("scheduled message purge job: missing repository or clock")
	}
	return &purgeJob{repo: repo, clock: clock}, nil
}

// Execute drops terminal rows past the retention window. Pending and in-flight
// messages are never touched, however old: an undelivered message is not litter.
func (j *purgeJob) Execute(_ context.Context) error {
	cutoff := j.clock.Now().Add(-terminalRetention)

	removed, err := j.repo.PurgeTerminalBefore(cutoff)
	if err != nil {
		return err
	}
	if removed > 0 {
		log.Printf("[scheduled_message] purged %d message(s) finished before %s",
			removed, cutoff.Format(time.RFC3339))
	}
	return nil
}

var _ sm.PurgeJob = (*purgeJob)(nil)
