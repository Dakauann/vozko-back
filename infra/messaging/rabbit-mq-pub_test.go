package queue

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type testPubHelper struct {
	pool       *ConnectionPool
	pub        *RabbitMQQueuePub
	mu         sync.Mutex
	connectors []*mockPubConnector
}

type mockPubConnector struct {
	mu     sync.Mutex
	closed bool
}

func (c *mockPubConnector) openChannel() (*amqp.Channel, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, errors.New("connection closed")
	}
	return &amqp.Channel{}, nil
}

func (c *mockPubConnector) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *mockPubConnector) close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func newTestPub() *testPubHelper {
	h := &testPubHelper{}
	h.pool = &ConnectionPool{
		connFactory: func() (amqpConnector, error) {
			c := &mockPubConnector{}
			h.mu.Lock()
			h.connectors = append(h.connectors, c)
			h.mu.Unlock()
			return c, nil
		},
		maxChPerConn: DefaultMaxChannelsPerConn,
	}
	h.pub = &RabbitMQQueuePub{
		pool:           h.pool,
		exchange:       "test-exchange",
		declaredTopics: make(map[string]struct{}),
	}
	return h
}

func (h *testPubHelper) lastConnector() *mockPubConnector {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.connectors) == 0 {
		return nil
	}
	return h.connectors[len(h.connectors)-1]
}

func safePublish(pub *RabbitMQQueuePub, topic string, msg []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("panic in publish")
		}
	}()
	return pub.Publish(topic, msg)
}

func safePublishWithDelay(pub *RabbitMQQueuePub, topic string, msg []byte, delay time.Duration) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("panic in publishWithDelay")
		}
	}()
	return pub.PublishWithDelay(topic, msg, delay)
}

func safeValidate(pub *RabbitMQQueuePub) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("panic in validate")
		}
	}()
	return pub.ValidateConnection()
}

func TestPub_MutexHeldDuringPublish(t *testing.T) {
	h := newTestPub()

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_ = safePublish(h.pub, "test-topic", []byte("payload"))
		}()
	}

	wg.Wait()

}

func TestPub_InvalidateRecovery(t *testing.T) {
	h := newTestPub()

	err := safePublish(h.pub, "topic", []byte("msg"))
	if err == nil {
		t.Fatal("expected error from mock channel")
	}

	h.pub.mu.Lock()
	if h.pub.channel != nil {
		t.Fatal("expected channel to be nil after invalidation")
	}
	if h.pub.release != nil {
		t.Fatal("expected release to be nil after invalidation")
	}
	h.pub.mu.Unlock()

	_, chans := h.pool.Stats()
	if chans != 0 {
		t.Fatalf("expected 0 channels in pool after invalidation, got %d", chans)
	}
}

func TestPub_CloseReleasesChannel(t *testing.T) {
	h := newTestPub()

	_ = safeValidate(h.pub)

	h.pub.Close()

	_, chans := h.pool.Stats()
	if chans != 0 {
		t.Fatalf("expected 0 channels after Close, got %d", chans)
	}
}

func TestPub_ConcurrentPublishAndPublishWithDelay(t *testing.T) {
	h := newTestPub()

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			if idx%2 == 0 {
				_ = safePublish(h.pub, "topic-a", []byte("msg"))
			} else {
				_ = safePublishWithDelay(h.pub, "topic-b", []byte("msg"), 5*time.Second)
			}
		}(i)
	}

	wg.Wait()
}

func TestPub_ConcurrentPublishWithDelaySameTopic(t *testing.T) {
	h := newTestPub()

	const goroutines = 200
	var wg sync.WaitGroup
	var errCount atomic.Int64

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			err := safePublishWithDelay(h.pub, "campaign.abc123", []byte(`{"id":"test"}`), 3*time.Second)
			if err != nil {
				errCount.Add(1)
			}
		}()
	}

	wg.Wait()

	if errCount.Load() == 0 {
		t.Fatal("expected errors from mock channel, got none")
	}
}

func TestPub_ValidateConnectionCreatesChannel(t *testing.T) {
	h := newTestPub()

	err := safeValidate(h.pub)
	if err == nil {
		t.Fatal("expected error from mock (Confirm on zero-value channel)")
	}

	if len(h.connectors) == 0 {
		t.Fatal("expected at least one connector to have been created")
	}
}

func TestPub_PoolDialFailure(t *testing.T) {
	pool := &ConnectionPool{
		connFactory: func() (amqpConnector, error) {
			return nil, errors.New("connection refused")
		},
		maxChPerConn: DefaultMaxChannelsPerConn,
	}
	pub := &RabbitMQQueuePub{
		pool:     pool,
		exchange: "test",
	}

	err := safePublish(pub, "topic", []byte("msg"))
	if err == nil {
		t.Fatal("expected dial error")
	}
}

func TestPub_PublishWithDelayZeroDuration(t *testing.T) {
	h := newTestPub()

	_ = safePublishWithDelay(h.pub, "topic", []byte("msg"), 0)
	_ = safePublishWithDelay(h.pub, "topic", []byte("msg"), -5*time.Second)
}

func TestPub_SequentialInvalidateAndRecover(t *testing.T) {
	h := newTestPub()

	for i := 0; i < 10; i++ {
		err := safePublish(h.pub, "topic", []byte("msg"))
		if err == nil {
			t.Fatal("expected error from mock channel")
		}

	}

	_, chans := h.pool.Stats()
	if chans != 0 {
		t.Fatalf("expected 0 channels after sequential failures, got %d", chans)
	}
}

func TestPub_ConnectionDiesAfterChannelCreated(t *testing.T) {
	h := newTestPub()

	_ = safePublish(h.pub, "topic", []byte("msg"))

	if len(h.connectors) == 0 {
		t.Fatal("expected at least one connector")
	}

	h.lastConnector().mu.Lock()
	h.lastConnector().closed = true
	h.lastConnector().mu.Unlock()

	err := safePublish(h.pub, "topic", []byte("msg"))
	if err == nil {
		t.Fatal("expected error")
	}

	h.mu.Lock()
	connCount := len(h.connectors)
	h.mu.Unlock()
	if connCount < 2 {
		t.Fatalf("expected at least 2 connectors (original + recovery), got %d", connCount)
	}
}

func TestPub_CloseIdempotent(t *testing.T) {
	h := newTestPub()

	_ = safeValidate(h.pub)

	for i := 0; i < 5; i++ {
		if err := h.pub.Close(); err != nil {
			t.Fatalf("Close() returned error: %v", err)
		}
	}
}

func TestPub_ConcurrentPublishAndClose(t *testing.T) {
	h := newTestPub()

	var wg sync.WaitGroup
	wg.Add(51)

	for i := 0; i < 50; i++ {
		go func() {
			defer wg.Done()
			_ = safePublish(h.pub, "topic", []byte("msg"))
		}()
	}

	go func() {
		defer wg.Done()
		time.Sleep(time.Millisecond)
		_ = h.pub.Close()
	}()

	wg.Wait()
}
