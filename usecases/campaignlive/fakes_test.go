package campaignlive

import (
	"context"
	"sync"
	"time"

	"vozko/domain/campaign"
)

type memoryMemo struct {
	mu      sync.Mutex
	values  map[string][]byte
	ttls    map[string]time.Duration
	compute int
}

func newMemoryMemo() *memoryMemo {
	return &memoryMemo{values: map[string][]byte{}, ttls: map[string]time.Duration{}}
}

func (m *memoryMemo) Remember(ctx context.Context, key string, ttl time.Duration, compute func(context.Context) ([]byte, error)) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if value, ok := m.values[key]; ok {
		return value, nil
	}
	m.compute++
	value, err := compute(ctx)
	if err != nil {
		return nil, err
	}
	m.values[key] = value
	m.ttls[key] = ttl
	return value, nil
}

func (m *memoryMemo) expire() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values = map[string][]byte{}
}

type memoryState struct {
	mu     sync.Mutex
	values map[string]string
}

func newMemoryState() *memoryState { return &memoryState{values: map[string]string{}} }

func (s *memoryState) SetString(key, value string, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = value
	return nil
}

func (s *memoryState) GetString(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.values[key], nil
}

type countingGate struct{ acquired int }

func (g *countingGate) Acquire(context.Context) (func(), error) {
	g.acquired++
	return func() {}, nil
}

func running(id string) campaign.LiveCampaign {
	return campaign.LiveCampaign{ID: id, WorkspaceID: "ws-1", Status: campaign.StatusRunning}
}
