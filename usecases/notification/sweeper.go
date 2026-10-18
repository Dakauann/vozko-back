package notification_usecase

import (
	"context"
	"errors"
	"time"

	"vozko/domain/notification"
)

const (
	StaleAfter       = 5 * time.Minute
	sweepBatch       = 500
	RetentionPeriod  = 180 * 24 * time.Hour
	maxPurgeBatches  = 200
	DefaultPurgeSize = 1000
)

type SweeperDeps struct {
	Outbox     notification.Outbox
	Broadcasts notification.BroadcastStore
	Queue      notification.Queue
	Now        func() time.Time
}

type Sweeper struct {
	outbox     notification.Outbox
	broadcasts notification.BroadcastStore
	queue      notification.Queue
	now        func() time.Time
}

func NewSweeper(d SweeperDeps) (*Sweeper, error) {
	if d.Outbox == nil || d.Broadcasts == nil || d.Queue == nil {
		return nil, errors.New("notification sweeper: outbox, broadcasts and queue are required")
	}
	return &Sweeper{outbox: d.Outbox, broadcasts: d.Broadcasts, queue: d.Queue, now: clockOrNow(d.Now)}, nil
}

func (s *Sweeper) Sweep(ctx context.Context) error {
	cutoff := s.now().Add(-StaleAfter)
	pending, err := s.outbox.StalePending(ctx, cutoff, sweepBatch)
	if err != nil {
		return err
	}
	var failures []error
	for _, id := range pending {
		failures = append(failures, s.queue.EnqueueFanout(ctx, id))
	}
	queued, err := s.broadcasts.StaleQueued(ctx, cutoff, sweepBatch)
	if err != nil {
		return errors.Join(append(failures, err)...)
	}
	for _, id := range queued {
		failures = append(failures, s.queue.EnqueueBroadcast(ctx, id))
	}
	return errors.Join(failures...)
}

type Retention struct {
	outbox notification.Outbox
	batch  int
	now    func() time.Time
}

func NewRetention(outbox notification.Outbox, batch int, now func() time.Time) *Retention {
	if batch <= 0 {
		batch = DefaultPurgeSize
	}
	return &Retention{outbox: outbox, batch: batch, now: clockOrNow(now)}
}

func (r *Retention) Purge(ctx context.Context) error {
	cutoff := r.now().Add(-RetentionPeriod)
	for i := 0; i < maxPurgeBatches; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		purged, err := r.outbox.PurgeBefore(ctx, cutoff, r.batch)
		if err != nil {
			return err
		}
		if purged < r.batch {
			return nil
		}
	}
	return nil
}
