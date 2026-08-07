package unofficial_whatsapp

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// The per-instance ceiling on profile enrichment.
//
// The staleness clock bounds how often ONE subject is read. It says nothing
// about how many subjects are read at once, and that gap is real: the webhook
// consumer runs messageConcurrency (20) workers, so a burst of messages from
// twenty people we have never seen — a broadcast reply wave, the first hour
// after a busy number connects — fires twenty parallel profile reads at a single
// WhatsApp account.
//
// On a channel whose failure mode is Meta disabling the customer's number, that
// is precisely the traffic shape to avoid. Twenty concurrent identity lookups is
// what a scraper looks like; a person opening a chat looks like one.
//
// The gate is NON-BLOCKING by design. When the budget is spent, enrichment is
// SKIPPED rather than queued:
//
//   - a webhook worker must never park waiting for a cosmetic read, or a burst
//     of new contacts becomes a burst of stalled workers and the messages
//     themselves stop landing;
//   - the staleness clock is only stamped when a read actually happens, so a
//     skipped subject stays stale and the next message retries it. The backlog
//     drains itself at the allowed rate with no queue to build, monitor or lose.
//
// The picture is late. The message is not. That is the correct trade for
// something the operator experiences as an avatar appearing a minute after the
// first message.

const (
	// profileReadsPerSecond is the sustained per-instance budget. Deliberately
	// modest: nothing here is urgent, and the cost of looking automated is the
	// customer's number.
	profileReadsPerSecond = 1
	// profileReadBurst lets a quiet number absorb a handful of new contacts
	// immediately rather than trickling the first few, which is the common case
	// and the one an operator watches.
	profileReadBurst = 5
)

// profileGate rate-limits enrichment per connected number.
//
// Per INSTANCE, not global: one workspace's busy number must not spend another
// workspace's budget, and the limit that matters is what a single WhatsApp
// account appears to be doing.
type profileGate struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter

	// perSecond and burst are fields rather than constants so a test can assert
	// the gate closes without waiting real seconds for it.
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

// allow reports whether this instance may spend one enrichment now.
//
// Never waits. A false answer means "not this time", and the caller's only
// correct response is to leave the subject stale so a later message retries it.
func (g *profileGate) allow(instanceID string) bool {
	if g == nil || instanceID == "" {
		// No gate configured is not a reason to stop enriching — the gate is a
		// safety limit, not a dependency.
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

// forget drops an instance's limiter.
//
// Called when an instance is deleted, so a platform that churns numbers does not
// accumulate one limiter per instance that ever existed. Cheap enough that
// leaking a few would not matter; explicit because "a map that only grows" is
// the kind of thing that is fine until it is a year later.
func (g *profileGate) forget(instanceID string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	delete(g.limiters, instanceID)
	g.mu.Unlock()
}

// sharedProfileGate is the process-wide gate.
//
// A package-level value because subjectProfile is copied by value into every
// usecase that enriches — the webhook handler and the group panel both hold
// one — and a per-copy limiter would let each of them spend the full budget
// independently, which is exactly the ceiling this exists to impose.
var sharedProfileGate = newProfileGate()

// profileGateWindow is how long a caller would have to wait for one token, used
// only for logging so a skipped enrichment is legible in the logs rather than
// looking like the read silently did nothing.
const profileGateWindow = time.Second / profileReadsPerSecond
