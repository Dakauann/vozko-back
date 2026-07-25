package queue

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
)

type mockConnector struct {
	mu         sync.Mutex
	closed     bool
	chanCount  int
	chanLimit  int
	channelErr error
}

func (m *mockConnector) openChannel() (*amqp.Channel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, fmt.Errorf("connection is closed")
	}
	if m.channelErr != nil {
		return nil, m.channelErr
	}
	if m.chanLimit > 0 && m.chanCount >= m.chanLimit {
		return nil, fmt.Errorf("channel limit reached")
	}
	m.chanCount++

	return &amqp.Channel{}, nil
}

func (m *mockConnector) isClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

func (m *mockConnector) close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

type testPoolHelper struct {
	pool  *ConnectionPool
	mu    sync.Mutex
	mocks []*mockConnector
}

func newTestPool(maxChPerConn int) *testPoolHelper {
	h := &testPoolHelper{}
	h.pool = &ConnectionPool{
		connFactory: func() (amqpConnector, error) {
			m := &mockConnector{}
			h.mu.Lock()
			h.mocks = append(h.mocks, m)
			h.mu.Unlock()
			return m, nil
		},
		maxChPerConn: maxChPerConn,
	}
	return h
}

func (h *testPoolHelper) mockCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.mocks)
}

func (h *testPoolHelper) mock(i int) *mockConnector {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.mocks[i]
}

func TestPool_SingleChannel(t *testing.T) {
	h := newTestPool(5)

	ch, release, err := h.pool.OpenChannel()
	if err != nil {
		t.Fatal(err)
	}
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}

	conns, chans := h.pool.Stats()
	if conns != 1 {
		t.Fatalf("expected 1 connection, got %d", conns)
	}
	if chans != 1 {
		t.Fatalf("expected 1 channel, got %d", chans)
	}

	release()

	_, chans = h.pool.Stats()
	if chans != 0 {
		t.Fatalf("expected 0 channels after release, got %d", chans)
	}
}

func TestPool_ChannelDistribution(t *testing.T) {
	maxCh := 3
	h := newTestPool(maxCh)

	releases := make([]func(), 0)

	for i := 0; i < maxCh; i++ {
		_, rel, err := h.pool.OpenChannel()
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, rel)
	}

	if h.mockCount() != 1 {
		t.Fatalf("expected 1 connection, got %d", h.mockCount())
	}

	_, rel, err := h.pool.OpenChannel()
	if err != nil {
		t.Fatal(err)
	}
	releases = append(releases, rel)

	if h.mockCount() != 2 {
		t.Fatalf("expected 2 connections, got %d", h.mockCount())
	}

	conns, chans := h.pool.Stats()
	if conns != 2 {
		t.Fatalf("expected 2 conns, got %d", conns)
	}
	if chans != maxCh+1 {
		t.Fatalf("expected %d channels, got %d", maxCh+1, chans)
	}

	for _, r := range releases {
		r()
	}

	_, chans = h.pool.Stats()
	if chans != 0 {
		t.Fatalf("expected 0 channels after releasing all, got %d", chans)
	}
}

func TestPool_AutoScale(t *testing.T) {
	maxCh := 2
	h := newTestPool(maxCh)
	total := 10

	releases := make([]func(), total)
	for i := 0; i < total; i++ {
		_, rel, err := h.pool.OpenChannel()
		if err != nil {
			t.Fatal(err)
		}
		releases[i] = rel
	}

	expectedConns := total / maxCh
	conns, chans := h.pool.Stats()
	if conns != expectedConns {
		t.Fatalf("expected %d connections, got %d", expectedConns, conns)
	}
	if chans != total {
		t.Fatalf("expected %d channels, got %d", total, chans)
	}

	for _, r := range releases {
		r()
	}
}

func TestPool_ReleaseIdempotent(t *testing.T) {
	h := newTestPool(5)

	_, release, err := h.pool.OpenChannel()
	if err != nil {
		t.Fatal(err)
	}

	release()
	release()
	release()

	_, chans := h.pool.Stats()
	if chans != 0 {
		t.Fatalf("expected 0 channels, got %d", chans)
	}
}

func TestPool_DeadConnectionPruning(t *testing.T) {
	h := newTestPool(5)

	_, rel, err := h.pool.OpenChannel()
	if err != nil {
		t.Fatal(err)
	}
	rel()

	if h.mockCount() != 1 {
		t.Fatal("expected 1 mock connection")
	}

	h.mock(0).mu.Lock()
	h.mock(0).closed = true
	h.mock(0).mu.Unlock()

	_, rel2, err := h.pool.OpenChannel()
	if err != nil {
		t.Fatal(err)
	}
	defer rel2()

	if h.mockCount() != 2 {
		t.Fatalf("expected 2 mocks (1 pruned + 1 new), got %d", h.mockCount())
	}

	conns, chans := h.pool.Stats()
	if conns != 1 {
		t.Fatalf("expected 1 live connection, got %d", conns)
	}
	if chans != 1 {
		t.Fatalf("expected 1 channel, got %d", chans)
	}
}

func TestPool_ChannelErrorRemovesConnection(t *testing.T) {
	h := newTestPool(5)

	_, rel, err := h.pool.OpenChannel()
	if err != nil {
		t.Fatal(err)
	}
	rel()

	h.mock(0).mu.Lock()
	h.mock(0).channelErr = fmt.Errorf("simulated channel error")
	h.mock(0).mu.Unlock()

	_, _, err = h.pool.OpenChannel()
	if err == nil {
		t.Fatal("expected error when openChannel fails")
	}

	conns, _ := h.pool.Stats()
	if conns != 0 {
		t.Fatalf("expected 0 connections after broken channel, got %d", conns)
	}

	h.mock(0).mu.Lock()
	h.mock(0).channelErr = nil
	h.mock(0).mu.Unlock()

	_, rel2, err := h.pool.OpenChannel()
	if err != nil {
		t.Fatal(err)
	}
	defer rel2()

	conns, _ = h.pool.Stats()
	if conns != 1 {
		t.Fatalf("expected 1 connection after recovery, got %d", conns)
	}
}

func TestPool_DialFailure(t *testing.T) {
	callCount := 0
	pool := &ConnectionPool{
		connFactory: func() (amqpConnector, error) {
			callCount++
			return nil, fmt.Errorf("connection refused")
		},
		maxChPerConn: 5,
	}

	_, _, err := pool.OpenChannel()
	if err == nil {
		t.Fatal("expected dial error")
	}
	if callCount != 1 {
		t.Fatalf("expected 1 dial attempt, got %d", callCount)
	}
}

func TestPool_Close(t *testing.T) {
	h := newTestPool(5)

	releases := make([]func(), 3)
	for i := 0; i < 3; i++ {
		_, rel, err := h.pool.OpenChannel()
		if err != nil {
			t.Fatal(err)
		}
		releases[i] = rel
	}

	if err := h.pool.Close(); err != nil {
		t.Fatal(err)
	}

	conns, _ := h.pool.Stats()
	if conns != 0 {
		t.Fatalf("expected 0 connections after close, got %d", conns)
	}

	_, _, err := h.pool.OpenChannel()
	if err != ErrPoolClosed {
		t.Fatalf("expected ErrPoolClosed, got %v", err)
	}

	if !h.mock(0).isClosed() {
		t.Fatal("expected mock connection to be closed")
	}
}

func TestPool_ReusesConnectionAfterRelease(t *testing.T) {
	h := newTestPool(2)

	_, rel1, _ := h.pool.OpenChannel()
	_, rel2, _ := h.pool.OpenChannel()

	if h.mockCount() != 1 {
		t.Fatalf("expected 1 connection, got %d", h.mockCount())
	}

	rel1()

	_, rel3, _ := h.pool.OpenChannel()

	if h.mockCount() != 1 {
		t.Fatalf("expected 1 connection (reused), got %d", h.mockCount())
	}

	rel2()
	rel3()
}

func TestPool_ConcurrentAccess(t *testing.T) {
	h := newTestPool(10)

	const goroutines = 100
	const channelsPerGoroutine = 50

	var wg sync.WaitGroup
	var errCount atomic.Int64

	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			releases := make([]func(), channelsPerGoroutine)
			for i := 0; i < channelsPerGoroutine; i++ {
				_, rel, err := h.pool.OpenChannel()
				if err != nil {
					errCount.Add(1)
					return
				}
				releases[i] = rel
			}
			for _, rel := range releases {
				rel()
			}
		}()
	}

	wg.Wait()

	if errCount.Load() != 0 {
		t.Fatalf("unexpected errors: %d", errCount.Load())
	}

	totalChannels := goroutines * channelsPerGoroutine
	expectedConns := (totalChannels + 9) / 10

	conns, chans := h.pool.Stats()
	if chans != 0 {
		t.Fatalf("expected 0 channels after all released, got %d", chans)
	}

	if conns == 0 {
		t.Fatal("expected at least 1 connection, got 0")
	}
	if conns > expectedConns {
		t.Fatalf("created more connections (%d) than theoretical max (%d)", conns, expectedConns)
	}
}

func TestPool_ScaleTo2000Channels(t *testing.T) {
	maxCh := 2000
	h := newTestPool(maxCh)

	total := 6000

	releases := make([]func(), total)
	for i := 0; i < total; i++ {
		_, rel, err := h.pool.OpenChannel()
		if err != nil {
			t.Fatalf("failed at channel %d: %v", i, err)
		}
		releases[i] = rel
	}

	conns, chans := h.pool.Stats()
	if conns != 3 {
		t.Fatalf("expected 3 connections for 6000 channels, got %d", conns)
	}
	if chans != total {
		t.Fatalf("expected %d channels, got %d", total, chans)
	}

	for i := 0; i < total/2; i++ {
		releases[i]()
	}

	_, chans = h.pool.Stats()
	if chans != total/2 {
		t.Fatalf("expected %d channels after half released, got %d", total/2, chans)
	}

	moreReleases := make([]func(), 4000)
	for i := 0; i < 4000; i++ {
		_, rel, err := h.pool.OpenChannel()
		if err != nil {
			t.Fatalf("failed at additional channel %d: %v", i, err)
		}
		moreReleases[i] = rel
	}

	conns, chans = h.pool.Stats()

	if conns != 4 {
		t.Fatalf("expected 4 connections, got %d", conns)
	}
	if chans != 7000 {
		t.Fatalf("expected 7000 channels, got %d", chans)
	}

	for i := total / 2; i < total; i++ {
		releases[i]()
	}
	for _, rel := range moreReleases {
		rel()
	}
}

func TestPool_ExhaustiveLifecycle(t *testing.T) {
	h := newTestPool(3)

	releases := make([]func(), 9)
	for i := 0; i < 9; i++ {
		_, rel, err := h.pool.OpenChannel()
		if err != nil {
			t.Fatal(err)
		}
		releases[i] = rel
	}

	conns, chans := h.pool.Stats()
	if conns != 3 || chans != 9 {
		t.Fatalf("phase 1: expected 3 conns/9 chans, got %d/%d", conns, chans)
	}

	h.mock(1).mu.Lock()
	h.mock(1).closed = true
	h.mock(1).mu.Unlock()

	releases[3]()
	releases[4]()
	releases[5]()

	for i := 0; i < 3; i++ {
		_, rel, err := h.pool.OpenChannel()
		if err != nil {
			t.Fatal(err)
		}
		releases[3+i] = rel
	}

	conns, chans = h.pool.Stats()

	if conns < 2 {
		t.Fatalf("phase 3: expected at least 2 conns, got %d", conns)
	}

	if err := h.pool.Close(); err != nil {
		t.Fatal(err)
	}

	_, _, err := h.pool.OpenChannel()
	if err != ErrPoolClosed {
		t.Fatalf("expected ErrPoolClosed after close, got %v", err)
	}
}
