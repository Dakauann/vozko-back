package emailsending

import (
	"errors"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

var errSharedDown = errors.New("redis: connection refused")

type memSendState struct {
	mu      sync.Mutex
	values  map[string]string
	sets    map[string]map[string]bool
	failing atomic.Bool
}

func newMemSendState() *memSendState {
	return &memSendState{values: map[string]string{}, sets: map[string]map[string]bool{}}
}

func (m *memSendState) down() error {
	if m.failing.Load() {
		return errSharedDown
	}
	return nil
}

func (m *memSendState) GetString(key string) (string, error) {
	if err := m.down(); err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.values[key], nil
}

func (m *memSendState) SetString(key, value string, _ time.Duration) error {
	if err := m.down(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[key] = value
	return nil
}

func (m *memSendState) SetNX(key, value string, _ time.Duration) (bool, error) {
	if err := m.down(); err != nil {
		return false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.values[key]; ok {
		return false, nil
	}
	m.values[key] = value
	return true, nil
}

func (m *memSendState) Del(keys ...string) error {
	if err := m.down(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range keys {
		delete(m.values, k)
	}
	return nil
}

func (m *memSendState) add(key string, delta int64) int64 {
	n, _ := strconv.ParseInt(m.values[key], 10, 64)
	n += delta
	m.values[key] = strconv.FormatInt(n, 10)
	return n
}

func (m *memSendState) IncrWithTTL(key string, _ time.Duration) (int64, error) {
	if err := m.down(); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.add(key, 1), nil
}

func (m *memSendState) TryIncrBy(key string, delta, max int64) (bool, error) {
	if err := m.down(); err != nil {
		return false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.add(key, delta) > max {
		m.add(key, -delta)
		return false, nil
	}
	return true, nil
}

func (m *memSendState) DecrBy(key string, amount int64) (int64, error) {
	if err := m.down(); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.add(key, -amount), nil
}

func (m *memSendState) SAdd(key string, members ...string) error {
	if err := m.down(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sets[key] == nil {
		m.sets[key] = map[string]bool{}
	}
	for _, member := range members {
		m.sets[key][member] = true
	}
	return nil
}

func (m *memSendState) Expire(string, time.Duration) (bool, error) {
	return true, m.down()
}

func (m *memSendState) SMembers(key string) ([]string, error) {
	if err := m.down(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for member := range m.sets[key] {
		out = append(out, member)
	}
	sort.Strings(out)
	return out, nil
}

func (m *memSendState) count(key string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, _ := strconv.ParseInt(m.values[key], 10, 64)
	return n
}
