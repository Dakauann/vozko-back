package audiencetest

import (
	"sync"
	"time"

	"vozko/domain/cache"
)

type State struct {
	cache.SharedState
	mu     sync.Mutex
	values map[string]string
	Err    error
}

func NewState() *State { return &State{values: map[string]string{}} }

func (s *State) SetNX(key, value string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return false, s.Err
	}
	if _, taken := s.values[key]; taken {
		return false, nil
	}
	s.values[key] = value
	return true, nil
}

func (s *State) SetString(key, value string, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return s.Err
	}
	s.values[key] = value
	return nil
}

func (s *State) GetString(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.values[key], s.Err
}

func (s *State) DelIfValue(key, value string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.values[key] != value {
		return false, nil
	}
	delete(s.values, key)
	return true, nil
}

func Flight(state *State) cache.SharedFlight {
	return cache.SharedFlight{State: state, Owner: state, TTL: time.Minute, Poll: time.Millisecond, Keep: time.Minute}
}
