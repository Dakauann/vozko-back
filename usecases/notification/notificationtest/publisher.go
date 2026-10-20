package notificationtest

import (
	"context"
	"sync"

	"vozko/domain/notification"
)

type Resolution struct {
	WorkspaceID string
	Keys        []string
}

type Publisher struct {
	mu          sync.Mutex
	drafts      []notification.Draft
	resolutions []Resolution
	Err         error
	ResolveErr  error
}

var _ notification.EpisodePublisher = (*Publisher)(nil)

func (p *Publisher) Publish(_ context.Context, d notification.Draft) (notification.Record, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.drafts = append(p.drafts, d)
	return notification.Record{Draft: d, ID: "n-test"}, p.Err
}

func (p *Publisher) Resolve(_ context.Context, workspaceID string, episodeKeys ...string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.resolutions = append(p.resolutions, Resolution{WorkspaceID: workspaceID, Keys: append([]string(nil), episodeKeys...)})
	return p.ResolveErr
}

func (p *Publisher) Drafts() []notification.Draft {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]notification.Draft(nil), p.drafts...)
}

func (p *Publisher) Resolutions() []Resolution {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Resolution(nil), p.resolutions...)
}

func (p *Publisher) ResolvedKeys() map[string]bool {
	out := map[string]bool{}
	for _, r := range p.Resolutions() {
		for _, k := range r.Keys {
			out[k] = true
		}
	}
	return out
}

func (p *Publisher) Only(t interface {
	Helper()
	Fatalf(string, ...interface{})
}) notification.Draft {
	t.Helper()
	drafts := p.Drafts()
	if len(drafts) != 1 {
		t.Fatalf("published %d notifications, want 1", len(drafts))
	}
	if err := drafts[0].Normalize().Validate(); err != nil {
		t.Fatalf("published an invalid notification: %v (%+v)", err, drafts[0])
	}
	return drafts[0]
}
