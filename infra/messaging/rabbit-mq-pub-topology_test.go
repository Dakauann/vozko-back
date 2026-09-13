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

// A publish must not report success for a message the broker throws away.
//
// The exchange is direct, so a message whose routing key matches no binding is
// discarded, and publisher confirms ACK it while it is discarded: a confirm
// answers "the exchange took it", never "a queue holds it". A publisher that
// ran before its consumer subscribed was therefore told every message landed
// while every one of them was dropped. So the publisher declares the topic's
// queue itself, and boundTopics is what keeps that to one round trip per topic
// instead of one per message.
func TestPub_BoundTopicsInitialized(t *testing.T) {
	pool := &ConnectionPool{maxChPerConn: DefaultMaxChannelsPerConn}
	pub := NewRabbitMQQueuePub(pool, "test-exchange").(*RabbitMQQueuePub)
	if pub.boundTopics == nil {
		t.Fatal("boundTopics map must be non-nil after construction")
	}
	if len(pub.boundTopics) != 0 {
		t.Fatalf("boundTopics must start empty, got %d entries", len(pub.boundTopics))
	}
}

// The cache describes topology on ONE channel. A reconnection may reach a
// broker that has never seen these queues, so remembering them across an
// invalidation is how a publisher goes back to dropping messages.
func TestPub_InvalidateClearsBoundTopics(t *testing.T) {
	h := newTestPub()
	h.pub.boundTopics = map[string]struct{}{"topic-a": {}, "topic-b": {}}

	h.pub.mu.Lock()
	h.pub.invalidateChannelLocked()
	h.pub.mu.Unlock()

	if got := len(h.pub.boundTopics); got != 0 {
		t.Fatalf("boundTopics must be cleared after invalidation, got %d entries", got)
	}
}

// Declaring costs a round trip, so a topic already declared on this channel must
// not pay for one. Proven by leaving no pool to open a channel from: a cached
// topic has to answer without reaching for one.
func TestPub_EnsureRoutableIsCachedPerTopic(t *testing.T) {
	pub := &RabbitMQQueuePub{
		exchange:       "test-exchange",
		declaredTopics: make(map[string]struct{}),
		boundTopics:    map[string]struct{}{"already-there": {}},
	}

	pub.mu.Lock()
	err := pub.ensureRoutableLocked("already-there")
	pub.mu.Unlock()

	if err != nil {
		t.Fatalf("a cached topic must not redeclare: %v", err)
	}
}
