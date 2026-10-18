package unofficial_whatsapp

import (
	"sync"

	"golang.org/x/time/rate"
)

const (
	profileReadsPerSecond = 1
	profileReadBurst      = 5
)

type profileGate struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter

	perSecond rate.Limit
	burst     int
}

func newProfileGate() *profileGate {
	return &profileGate{
		limiters:  make(map[string]*rate.Limiter),
		perSecond: rate.Limit(profileReadsPerSecond),
		burst:     profileReadBurst,
	}
}

func (g *profileGate) allow(instanceID string) bool {
	if g == nil || instanceID == "" {
		return true
	}

	g.mu.Lock()
	limiter, ok := g.limiters[instanceID]
	if !ok {
		limiter = rate.NewLimiter(g.perSecond, g.burst)
		g.limiters[instanceID] = limiter
	}
	g.mu.Unlock()

	return limiter.Allow()
}

var sharedProfileGate = newProfileGate()
