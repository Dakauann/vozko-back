package unofficial_whatsapp_campaign

import (
	"fmt"
	"math/rand"
	"time"

	"vozko/domain/cache"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

// The two ban-avoidance controls that have to work ACROSS processes.
//
// Both live in Redis rather than in the consumer, and that is the whole point.
// A workspace can have several campaigns pointed at one number and several
// consumer replicas running, so a per-process counter or a per-process sleep
// would let N replicas each send at the configured rate — N times too fast, at
// N times the daily cap, against a number whose owner loses their WhatsApp if we
// get it wrong.

// SendBudget enforces pacing and the daily cap for one connected number.
type SendBudget struct {
	shared cache.SharedState
	// jitter picks the delay inside a range. Injectable so a test can assert the
	// bounds without depending on randomness.
	jitter func(minMS, maxMS int) int
	now    func() time.Time
}

func NewSendBudget(sharedState cache.SharedState) *SendBudget {
	return &SendBudget{
		shared: sharedState,
		jitter: defaultJitter,
		now:    func() time.Time { return time.Now().UTC() },
	}
}

// dailyKey buckets by UTC day.
//
// UTC rather than the workspace's timezone deliberately: WhatsApp's own limits
// are not local, and a tenant-local reset would let a workspace spanning
// timezones get two allowances out of one number.
func dailyKey(instanceID string, at time.Time) string {
	return fmt.Sprintf("uw:sendbudget:%s:%s", instanceID, at.Format("2006-01-02"))
}

func paceLeaseKey(instanceID string) string {
	return fmt.Sprintf("uw:pacelease:%s", instanceID)
}

// budgetTTL keeps a day's counter alive well past midnight so a campaign running
// through the boundary cannot be handed a second allowance by an early expiry.
const budgetTTL = 48 * time.Hour

// TryConsumeDaily reserves one send against the number's daily allowance.
//
// Increment-then-check rather than check-then-increment: the second races, and
// under a fan-out of concurrent consumers the race is not theoretical. An
// over-count is corrected by the rollback below, and erring toward refusing a
// send is the right direction on this channel.
//
// A cap of zero means unlimited HERE — but only because the caller has already
// resolved it through Campaign.EffectiveDailyCap, which never returns zero
// unless both the campaign and the instance are explicitly unlimited.
func (b *SendBudget) TryConsumeDaily(instanceID string, cap int) (bool, error) {
	if cap <= 0 {
		return true, nil
	}
	key := dailyKey(instanceID, b.now())
	used, err := b.shared.IncrWithTTL(key, budgetTTL)
	if err != nil {
		// Fail CLOSED. A cache we cannot reach is not permission to blast: the
		// message is requeued and tried again shortly.
		return false, err
	}
	if used > int64(cap) {
		// Undo the over-count so a long pause does not leave the counter
		// permanently above the cap and lock the number out for the rest of the day.
		_, _ = b.shared.Decr(key)
		return false, nil
	}
	return true, nil
}

// ReleaseDaily returns an unspent reservation.
//
// Called when a send was budgeted for and then did not happen — a number that
// turned out not to be on WhatsApp, a spam-window skip. Without it, a list that
// is 30% dead would burn 30% of a number's daily allowance on messages nobody
// ever received.
func (b *SendBudget) ReleaseDaily(instanceID string, cap int) {
	if cap <= 0 {
		return
	}
	_, _ = b.shared.Decr(dailyKey(instanceID, b.now()))
}

// UsedToday reports the number's consumption, for the UI.
func (b *SendBudget) UsedToday(instanceID string) int64 {
	raw, err := b.shared.GetString(dailyKey(instanceID, b.now()))
	if err != nil || raw == "" {
		return 0
	}
	var n int64
	_, _ = fmt.Sscanf(raw, "%d", &n)
	return n
}

// AcquirePace takes the per-instance pacing lease.
//
// A lease rather than a sleep, because the constraint is per NUMBER and not per
// consumer: several campaigns can target one number and several replicas can be
// running, and a local sleep would let each of them send at the full rate.
//
// SETNX with a jittered TTL means whoever wins sends now and everyone else is
// told how long to wait. It degrades to "everyone waits" if Redis is
// unreachable, which is the correct direction — the alternative is "everyone
// sends".
//
// Returns (acquired, waitFor).
func (b *SendBudget) AcquirePace(instanceID string, minMS, maxMS int) (bool, time.Duration) {
	delay := time.Duration(b.jitter(minMS, maxMS)) * time.Millisecond

	ok, err := b.shared.SetNX(paceLeaseKey(instanceID), "1", delay)
	if err != nil {
		return false, delay
	}
	if !ok {
		// Somebody else holds the lease. Wait a fresh jittered interval rather
		// than the lease's remaining TTL: every waiter reading the same TTL would
		// wake together and stampede the moment it expired.
		return false, delay
	}
	return true, 0
}

// PacingFor is the jitter range a campaign should use, clamped by the channel
// floor and never faster than the instance itself allows.
//
// The campaign may be SLOWER than its number but never faster: the ban risk
// belongs to the number, so its floor wins over whatever a campaign was
// configured with.
func PacingFor(c *uwc.Campaign, instance *uw.Instance) (minMS, maxMS int) {
	minMS, maxMS = c.SendDelayRange()
	instMin, _ := instance.SendDelayRange()
	if minMS < instMin {
		minMS = instMin
	}
	if maxMS < minMS {
		maxMS = minMS
	}
	return minMS, maxMS
}

func defaultJitter(minMS, maxMS int) int {
	if maxMS <= minMS {
		return minMS
	}
	return minMS + rand.Intn(maxMS-minMS+1)
}
