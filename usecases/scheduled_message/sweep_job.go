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
	// dueBatchLimit bounds one sweep tick. Saturation is logged rather than
	// swallowed: a sweep that quietly stops at its limit reads as "nothing due"
	// while it is in fact falling behind.
	dueBatchLimit = 500

	// stuckClaimAfter is how long a claim may sit before its dispatcher is
	// presumed dead. Generously longer than any real send, because retiring a
	// live dispatch would report a message as failed while it is on its way.
	stuckClaimAfter = 5 * time.Minute

	stuckBatchLimit = 200
)

type sweepJob struct {
	repo     sm.Repository
	dispatch sm.DispatchUseCase
	clock    sm.Clock
}

// NewSweepJob wires the backstop.
//
// The delayed queue is the primary trigger and this is what makes the feature
// correct without it: a broker outage during create, a lost or expired delayed
// message, a consumer that was down. Losing the queue costs latency; losing
// this would cost messages.
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

// dispatchDue delivers everything past due.
//
// The batch is claimed in one conditional write, so this overlapping with the
// queue consumer — or with another replica's sweep — cannot produce a duplicate
// send. Each claimed message is then delivered directly, without re-claiming.
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
			// One bad message must not strand the rest of the batch, all of
			// which are already claimed and would otherwise sit in `sending`
			// until the stuck-claim pass retires them.
			log.Printf("[scheduled_message] sweep could not dispatch %s: %v", message.ID, err)
		}
	}
	return nil
}

// retireStuckClaims closes out messages whose dispatcher died mid-send.
//
// They are marked failed and NEVER retried. We cannot tell a send that never
// left from one that reached the customer before the process died, and an
// unwanted duplicate is unrecoverable while a visible failure costs the
// operator one click. The reason is distinct so the UI can say "we could not
// confirm delivery" rather than the plain lie "failed".
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
