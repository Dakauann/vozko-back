package cache

import (
	"sync"
	"time"

	domain "vozko/domain/cache"
)

type memoryEntry struct {
	windowStart   time.Time
	requestsCount int
}

type memoryRateLimiter struct {
	maxRequests int
	window      time.Duration
	entries     map[string]*memoryEntry
	mu          sync.Mutex
}

func NewMemoryRateLimiter(_ string, maxRequests int, window time.Duration) domain.RateLimiter {
	return &memoryRateLimiter{
		maxRequests: maxRequests,
		window:      window,
		entries:     make(map[string]*memoryEntry),
	}
}

func (m *memoryRateLimiter) Allow(key string) (bool, time.Duration, error) {
	now := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()

	entry, exists := m.entries[key]

	if !exists {
		m.entries[key] = &memoryEntry{
			windowStart:   now,
			requestsCount: 1,
		}
		return true, 0, nil
	}

	if now.Sub(entry.windowStart) >= m.window {
		entry.windowStart = now
		entry.requestsCount = 1
		return true, 0, nil
	}

	if entry.requestsCount >= m.maxRequests {
		retryAfter := m.window - now.Sub(entry.windowStart)
		return false, retryAfter, nil
	}

	entry.requestsCount++
	return true, 0, nil
}
