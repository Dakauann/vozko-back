package queue

import (
	"sync"
	"time"
)

type deliveryGate struct {
	mu      sync.Mutex
	closing bool
	running sync.WaitGroup
}

func (g *deliveryGate) enter() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closing {
		return false
	}
	g.running.Add(1)
	return true
}

func (g *deliveryGate) leave() {
	g.running.Done()
}

func (g *deliveryGate) drain(timeout time.Duration) bool {
	g.mu.Lock()
	g.closing = true
	g.mu.Unlock()

	finished := make(chan struct{})
	go func() {
		g.running.Wait()
		close(finished)
	}()
	select {
	case <-finished:
		return true
	case <-time.After(timeout):
		return false
	}
}
