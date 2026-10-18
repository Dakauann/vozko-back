package notification_usecase

import (
	"context"
	"errors"
	"log"
	"time"

	"vozko/domain/notification"
)

type PublisherDeps struct {
	Outbox notification.Outbox
	Queue  notification.Queue
	Images notification.ImageStore
	Now    func() time.Time
}

type Publisher struct {
	outbox notification.Outbox
	queue  notification.Queue
	images notification.ImageStore
	now    func() time.Time
}

var _ notification.Publisher = (*Publisher)(nil)

func NewPublisher(d PublisherDeps) (*Publisher, error) {
	if d.Outbox == nil || d.Queue == nil || d.Images == nil {
		return nil, errors.New("notification publisher: outbox, queue and image store are required")
	}
	return &Publisher{outbox: d.Outbox, queue: d.Queue, images: d.Images, now: clockOrNow(d.Now)}, nil
}

func (p *Publisher) Publish(ctx context.Context, draft notification.Draft) (notification.Record, error) {
	draft = draft.Normalize()
	if err := draft.Validate(); err != nil {
		return notification.Record{}, err
	}
	if draft.ImageURL != "" && !p.images.Owns(draft.ImageURL) {
		return notification.Record{}, notification.ErrImageForeign
	}
	now := p.now()
	draft.DedupKey = draft.StoredDedupKey(now)
	stored, created, err := p.outbox.Create(ctx, notification.Record{Draft: draft, Status: notification.StatusPending, CreatedAt: now})
	if err != nil {
		return notification.Record{}, err
	}
	if !created {
		return stored, nil
	}
	if err := p.queue.EnqueueFanout(ctx, stored.ID); err != nil {
		log.Printf("[notifications] fanout of %s not queued, the sweeper will retry: %v", stored.ID, err)
	}
	return stored, nil
}

func clockOrNow(now func() time.Time) func() time.Time {
	if now != nil {
		return now
	}
	return func() time.Time { return time.Now().UTC() }
}
