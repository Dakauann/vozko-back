package unofficial_whatsapp_campaign

import (
	"fmt"
	"math/rand"
	"time"

	"vozko/domain/cache"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

type SendBudget struct {
	shared cache.SharedState
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

func dailyKey(instanceID string, at time.Time) string {
	return fmt.Sprintf("uw:sendbudget:%s:%s", instanceID, at.Format("2006-01-02"))
}

func paceLeaseKey(instanceID string) string {
	return fmt.Sprintf("uw:pacelease:%s", instanceID)
}

const budgetTTL = 48 * time.Hour

func (b *SendBudget) TryConsumeDaily(instanceID string, cap int) (bool, error) {
	if cap <= 0 {
		return true, nil
	}
	key := dailyKey(instanceID, b.now())
	used, err := b.shared.IncrWithTTL(key, budgetTTL)
	if err != nil {
		return false, err
	}
	if used > int64(cap) {
		_, _ = b.shared.Decr(key)
		return false, nil
	}
	return true, nil
}

func (b *SendBudget) ReleaseDaily(instanceID string, cap int) {
	if cap <= 0 {
		return
	}
	_, _ = b.shared.Decr(dailyKey(instanceID, b.now()))
}

func (b *SendBudget) UsedToday(instanceID string) int64 {
	raw, err := b.shared.GetString(dailyKey(instanceID, b.now()))
	if err != nil || raw == "" {
		return 0
	}
	var n int64
	_, _ = fmt.Sscanf(raw, "%d", &n)
	return n
}

func (b *SendBudget) AcquirePace(instanceID string, minMS, maxMS int) (bool, time.Duration) {
	delay := time.Duration(b.jitter(minMS, maxMS)) * time.Millisecond

	ok, err := b.shared.SetNX(paceLeaseKey(instanceID), "1", delay)
	if err != nil {
		return false, delay
	}
	if !ok {
		return false, delay
	}
	return true, 0
}

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
