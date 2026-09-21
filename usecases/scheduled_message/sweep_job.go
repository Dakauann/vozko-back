package scheduled_message_usecase

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	sm "vozko/domain/scheduled_message"
)

const (
	dueBatchLimit = 500

	stuckClaimAfter = 5 * time.Minute

	stuckBatchLimit = 200
)

type sweepJob struct {
	repo     sm.Repository
	dispatch sm.DispatchUseCase
	clock    sm.Clock
}

func NewSweepJob(repo sm.Repository, dispatch sm.DispatchUseCase, clock sm.Clock) (sm.SweepJob, error) {
	missing := []string{}
	if repo == nil {
		missing = append(missing, "repository")
	}
	if dispatch == nil {
		missing = append(missing, "dispatch use case")
	}
	if clock == nil {
		missing = append(missing, "clock")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("scheduled message sweep job: missing %s", strings.Join(missing, ", "))
	}
	return &sweepJob{repo: repo, dispatch: dispatch, clock: clock}, nil
}

func (j *sweepJob) Execute(ctx context.Context) error {
	now := j.clock.Now()

	if err := j.dispatchDue(ctx, now); err != nil {
		return err
	}
	return j.retireStuckClaims(now)
}

func (j *sweepJob) dispatchDue(ctx context.Context, now time.Time) error {
	due, err := j.repo.ClaimDueBatch(now, dueBatchLimit)
	if err != nil {
		return err
	}
	if len(due) == 0 {
		return nil
	}

	if len(due) == dueBatchLimit {
		log.Printf("[scheduled_message] sweep hit its batch limit of %d; more messages are due and will go out on the next tick",
			dueBatchLimit)
	}

	for _, message := range due {
		if err := j.dispatch.DispatchClaimed(ctx, message); err != nil {
			log.Printf("[scheduled_message] sweep could not dispatch %s: %v", message.ID, err)
		}
	}
	return nil
}

func (j *sweepJob) retireStuckClaims(now time.Time) error {
	stuck, err := j.repo.ListStuckClaims(now.Add(-stuckClaimAfter), stuckBatchLimit)
	if err != nil {
		return err
	}

	for _, message := range stuck {
		log.Printf("[scheduled_message] %s was claimed at %v and never finished; retiring as interrupted",
			message.ID, message.ClaimedAt)
		if err := j.repo.MarkFailed(message.ID, sm.ReasonDispatchInterrupted,
			"the delivery was interrupted and could not be confirmed"); err != nil {
			log.Printf("[scheduled_message] could not retire stuck claim %s: %v", message.ID, err)
		}
	}
	return nil
}

var _ sm.SweepJob = (*sweepJob)(nil)
