package notification_usecase

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"vozko/domain/notification"
)

type PublisherDeps struct {
	Outbox   notification.Outbox
	Queue    notification.Queue
	Images   notification.ImageStore
	Episodes notification.EpisodeStore
	Now      func() time.Time
}

type Publisher struct {
	outbox   notification.Outbox
	queue    notification.Queue
	images   notification.ImageStore
	episodes notification.EpisodeStore
	now      func() time.Time
}

var _ notification.EpisodePublisher = (*Publisher)(nil)

func NewPublisher(d PublisherDeps) (*Publisher, error) {
	if d.Outbox == nil || d.Queue == nil || d.Images == nil || d.Episodes == nil {
		return nil, errors.New("notification publisher: outbox, queue, image store and episode store are required")
	}
	return &Publisher{outbox: d.Outbox, queue: d.Queue, images: d.Images, episodes: d.Episodes, now: clockOrNow(d.Now)}, nil
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
	if draft.EpisodeKey != "" {
		return p.publishEpisodeStep(ctx, draft, now)
	}
	return p.store(ctx, draft, now)
}

func (p *Publisher) Resolve(ctx context.Context, workspaceID string, episodeKeys ...string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return notification.ErrWorkspaceRequired
	}
	keys := notification.EpisodeKeys(episodeKeys)
	if len(keys) == 0 {
		return nil
	}
	_, err := p.episodes.ResolveEpisodes(ctx, workspaceID, keys, p.now())
	return err
}

func (p *Publisher) store(ctx context.Context, draft notification.Draft, now time.Time) (notification.Record, error) {
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

func (p *Publisher) publishEpisodeStep(ctx context.Context, draft notification.Draft, now time.Time) (notification.Record, error) {
	episode, err := p.currentEpisode(ctx, draft.WorkspaceID, draft.EpisodeKey, now)
	if err != nil {
		return notification.Record{}, err
	}
	step, due := episode.NextStep(now)
	if !due {
		return notification.Record{}, nil
	}
	shaped := draft.ForEpisodeStep(step, episode.OpenedAt, brasilia)
	shaped.DedupKey = episode.StepKey(step)
	stored, err := p.store(ctx, shaped, now)
	if err != nil {
		return notification.Record{}, err
	}
	advanced, err := p.episodes.AdvanceEpisode(ctx, episode.ID, step, stored.ID, now)
	if err != nil {
		return notification.Record{}, err
	}
	if advanced {
		p.withdrawPreviousEntry(ctx, episode, stored.ID)
	}
	return stored, nil
}

func (p *Publisher) currentEpisode(ctx context.Context, workspaceID, key string, now time.Time) (notification.Episode, error) {
	episode, found, err := p.episodes.OpenEpisode(ctx, workspaceID, key)
	if err != nil {
		return notification.Episode{}, err
	}
	if found && episode.Quiet(now) {
		if err := p.episodes.CloseEpisode(ctx, episode.ID, now, notification.EpisodeWentQuiet); err != nil {
			return notification.Episode{}, err
		}
		found = false
	}
	if !found {
		return p.episodes.StartEpisode(ctx, notification.NewEpisode(workspaceID, key, now))
	}
	return episode, p.episodes.SeeEpisode(ctx, episode.ID, now)
}

func (p *Publisher) withdrawPreviousEntry(ctx context.Context, episode notification.Episode, currentID string) {
	if episode.NotificationID == "" || episode.NotificationID == currentID {
		return
	}
	if err := p.outbox.Withdraw(ctx, episode.NotificationID); err != nil {
		log.Printf("[notifications] the previous entry %s of episode %s stays in the bell: %v", episode.NotificationID, episode.ID, err)
	}
}

func clockOrNow(now func() time.Time) func() time.Time {
	if now != nil {
		return now
	}
	return func() time.Time { return time.Now().UTC() }
}
