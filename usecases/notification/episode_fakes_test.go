package notification_usecase

import (
	"context"
	"fmt"
	"sync"
	"time"

	"vozko/domain/notification"
)

func (m *memoryCenter) Withdraw(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.records, id)
	for key, r := range m.recipients {
		if r.NotificationID == id {
			delete(m.recipients, key)
		}
	}
	return nil
}

func (m *memoryCenter) liveRecords() []notification.Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []notification.Record
	for _, id := range m.order {
		if r, ok := m.records[id]; ok {
			out = append(out, r)
		}
	}
	return out
}

type memoryEpisodes struct {
	mu         sync.Mutex
	seq        int
	episodes   map[string]notification.Episode
	advanceErr error
	lostRace   bool
	purged     int
}

func newMemoryEpisodes() *memoryEpisodes {
	return &memoryEpisodes{episodes: map[string]notification.Episode{}}
}

func (m *memoryEpisodes) OpenEpisode(_ context.Context, workspaceID, key string) (notification.Episode, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.episodes {
		if e.WorkspaceID == workspaceID && e.Key == key && e.ClosedAt == nil {
			return e, true, nil
		}
	}
	return notification.Episode{}, false, nil
}

func (m *memoryEpisodes) StartEpisode(ctx context.Context, e notification.Episode) (notification.Episode, error) {
	if open, found, _ := m.OpenEpisode(ctx, e.WorkspaceID, e.Key); found {
		return open, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	e.ID = fmt.Sprintf("ep-%d", m.seq)
	m.episodes[e.ID] = e
	return e, nil
}

func (m *memoryEpisodes) SeeEpisode(_ context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.episodes[id]
	if at.After(e.LastSeenAt) {
		e.LastSeenAt = at
	}
	m.episodes[id] = e
	return nil
}

func (m *memoryEpisodes) AdvanceEpisode(_ context.Context, id string, step notification.EpisodeStep, notificationID string, at time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.advanceErr != nil {
		return false, m.advanceErr
	}
	if m.lostRace {
		return false, nil
	}
	e := m.episodes[id]
	if e.StepsSent != step-1 {
		return false, nil
	}
	e.StepsSent, e.NotificationID, e.LastSentAt = step, notificationID, &at
	m.episodes[id] = e
	return true, nil
}

func (m *memoryEpisodes) CloseEpisode(_ context.Context, id string, at time.Time, reason notification.EpisodeClose) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.episodes[id]
	if e.ClosedAt == nil {
		e.ClosedAt, e.CloseReason = &at, reason
	}
	m.episodes[id] = e
	return nil
}

func (m *memoryEpisodes) ResolveEpisodes(_ context.Context, workspaceID string, keys []string, at time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	wanted := map[string]bool{}
	for _, k := range keys {
		wanted[k] = true
	}
	n := 0
	for id, e := range m.episodes {
		if e.WorkspaceID == workspaceID && wanted[e.Key] && e.ClosedAt == nil {
			e.ClosedAt, e.CloseReason = &at, notification.EpisodeResolved
			m.episodes[id] = e
			n++
		}
	}
	return n, nil
}

func (m *memoryEpisodes) PurgeEpisodesBefore(_ context.Context, cutoff time.Time, limit int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for id, e := range m.episodes {
		if n == limit {
			break
		}
		last := e.LastSeenAt
		if e.ClosedAt != nil {
			last = *e.ClosedAt
		}
		if last.Before(cutoff) {
			delete(m.episodes, id)
			n++
		}
	}
	m.purged += n
	return n, nil
}

func (m *memoryEpisodes) all() []notification.Episode {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]notification.Episode, 0, len(m.episodes))
	for i := 1; i <= m.seq; i++ {
		if e, ok := m.episodes[fmt.Sprintf("ep-%d", i)]; ok {
			out = append(out, e)
		}
	}
	return out
}

type fixedClock struct{ at time.Time }

func (c *fixedClock) now() time.Time { return c.at }

func (c *fixedClock) advance(d time.Duration) { c.at = c.at.Add(d) }
