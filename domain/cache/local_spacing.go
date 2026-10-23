package cache

import (
	"sync"
	"time"
)

type LocalSpacing struct {
	mu    sync.Mutex
	every time.Duration
	last  time.Time
}

func NewLocalSpacing(every time.Duration) *LocalSpacing {
	return &LocalSpacing{every: every}
}

func (s *LocalSpacing) Take(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.last.IsZero() && now.Before(s.last.Add(s.every)) {
		return false
	}
	s.last = now
	return true
}

func (s *LocalSpacing) Mark(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if now.After(s.last) {
		s.last = now
	}
}
