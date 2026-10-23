package cache

import (
	"sync"
	"testing"
	"time"
)

func TestLocalSpacingGrantsOncePerInterval(t *testing.T) {
	start := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		marks []time.Duration
		at    time.Duration
		want  bool
	}{
		{name: "first take", at: 0, want: true},
		{name: "too soon", marks: []time.Duration{0}, at: 999 * time.Millisecond, want: false},
		{name: "a full interval later", marks: []time.Duration{0}, at: time.Second, want: true},
		{name: "a grant made elsewhere counts", marks: []time.Duration{500 * time.Millisecond}, at: time.Second, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewLocalSpacing(time.Second)
			for _, m := range tc.marks {
				s.Mark(start.Add(m))
			}
			if got := s.Take(start.Add(tc.at)); got != tc.want {
				t.Fatalf("Take = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLocalSpacingNeverGrantsTwiceInOneIntervalUnderConcurrency(t *testing.T) {
	s := NewLocalSpacing(time.Hour)
	now := time.Now()
	var granted sync.WaitGroup
	var mu sync.Mutex
	grants := 0
	for range 64 {
		granted.Add(1)
		go func() {
			defer granted.Done()
			if s.Take(now) {
				mu.Lock()
				grants++
				mu.Unlock()
			}
		}()
	}
	granted.Wait()
	if grants != 1 {
		t.Fatalf("grants = %d, want 1", grants)
	}
}
