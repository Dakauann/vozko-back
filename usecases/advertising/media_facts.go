package advertising

import (
	"context"
	"errors"
	"fmt"
	"sync"

	ads "vozko/domain/advertising"
	"vozko/domain/media"
)

const (
	factsCacheLimit   = 512
	codeMediaNotFound = "not_found"
)

var ErrUnreadableCreative = errors.New("ads: the creative file could not be read")

type CreativeProbe interface {
	Facts(kind ads.MediaKind, file ads.MediaFile) (ads.MediaFacts, error)
}

type factsCache struct {
	mu      sync.Mutex
	entries map[string]ads.MediaFacts
}

func newFactsCache() *factsCache {
	return &factsCache{entries: map[string]ads.MediaFacts{}}
}

func (c *factsCache) get(key string) (ads.MediaFacts, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	facts, ok := c.entries[key]
	return facts, ok
}

func (c *factsCache) put(key string, facts ads.MediaFacts) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= factsCacheLimit {
		clear(c.entries)
	}
	c.entries[key] = facts
}

func factsKey(workspaceID string, ref ads.MediaRef) string {
	return workspaceID + "/" + string(ref.Kind) + "/" + ref.MediaID
}

func (l *library) Facts(ctx context.Context, workspaceID string, ref ads.MediaRef) (ads.MediaFacts, error) {
	key := factsKey(workspaceID, ref)
	if facts, ok := l.facts.get(key); ok {
		return facts, nil
	}
	described, err := l.Describe(ctx, workspaceID, ref)
	if err != nil {
		return ads.MediaFacts{}, err
	}
	file, err := l.fetcher.Open(ctx, described.URL)
	if err != nil {
		return ads.MediaFacts{}, fmt.Errorf("ads: open creative media: %w", err)
	}
	facts, err := l.probe.Facts(ref.Kind, file)
	if err != nil {
		return ads.MediaFacts{}, errors.Join(ErrUnreadableCreative, err)
	}
	l.facts.put(key, facts)
	return facts, nil
}

func (c creativeMedia) issue(ctx context.Context, workspaceID string, ref ads.MediaRef) (string, error) {
	if IsMetaMedia(ref) {
		return "", nil
	}
	facts, err := c.source.Facts(ctx, workspaceID, ref)
	switch {
	case errors.Is(err, ads.ErrMediaNotImage), errors.Is(err, media.ErrMediaNotFound):
		return codeMediaNotFound, nil
	case errors.Is(err, ErrUnreadableCreative):
		return ads.CodeMediaUnreadable, nil
	case err != nil:
		return "", err
	}
	return facts.Issue(), nil
}

func (c creativeMedia) checkSpec(ctx context.Context, workspaceID string, refs []ads.MediaRef, field string) error {
	for _, ref := range refs {
		code, err := c.issue(ctx, workspaceID, ref)
		if err != nil {
			return err
		}
		if code != "" {
			return ads.FieldError(field, code)
		}
	}
	return nil
}
