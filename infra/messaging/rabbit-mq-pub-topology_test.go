package queue

import (
	"testing"
)

func TestPub_DeclaredTopicsInitialized(t *testing.T) {
	pool := &ConnectionPool{maxChPerConn: DefaultMaxChannelsPerConn}
	pub := NewRabbitMQQueuePub(pool, "test-exchange").(*RabbitMQQueuePub)
	if pub.declaredTopics == nil {
		t.Fatal("declaredTopics map must be non-nil after construction")
	}
	if len(pub.declaredTopics) != 0 {
		t.Fatalf("declaredTopics must start empty, got %d entries", len(pub.declaredTopics))
	}
}

func TestPub_InvalidateClearsDeclaredTopics(t *testing.T) {
	h := newTestPub()
	h.pub.declaredTopics = map[string]struct{}{
		"topic-a": {},
		"topic-b": {},
	}

	h.pub.mu.Lock()
	h.pub.invalidateChannelLocked()
	h.pub.mu.Unlock()

	if got := len(h.pub.declaredTopics); got != 0 {
		t.Fatalf("declaredTopics must be cleared after invalidation, got %d entries", got)
	}
}

func TestPub_DeclaredTopicsCacheSurvivesUntilInvalidate(t *testing.T) {
	h := newTestPub()

	h.pub.declaredTopics["topic-x"] = struct{}{}

	if _, ok := h.pub.declaredTopics["topic-x"]; !ok {
		t.Fatal("expected topic-x to be cached")
	}

	h.pub.mu.Lock()
	h.pub.invalidateChannelLocked()
	h.pub.mu.Unlock()

	if _, ok := h.pub.declaredTopics["topic-x"]; ok {
		t.Fatal("expected topic-x to be cleared after invalidate")
	}
}
