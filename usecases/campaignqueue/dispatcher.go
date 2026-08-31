package campaignqueue

import (
	"encoding/json"
	"fmt"

	"vozko/domain/cache"
	"vozko/domain/campaign"
	"vozko/domain/messaging"
)

// Dispatcher fans a campaign's pending entries onto its queue.
//
// The other half of the plumbing, split from Runner because the two have
// different lifetimes and different callers: a dispatch happens once when an
// operator presses Start, while a runner lives for as long as the process does.
type Dispatcher struct {
	pub    messaging.MessageQueuePub
	shared cache.SharedState
	ns     campaign.Namespace
}

func NewDispatcher(pub messaging.MessageQueuePub, sharedState cache.SharedState, ns campaign.Namespace) *Dispatcher {
	return &Dispatcher{pub: pub, shared: sharedState, ns: ns}
}

// Enqueue publishes one message per entry and arms the completion counter.
//
// The counter is set AFTER publishing, and that order is deliberate: setting it
// first and then failing to publish leaves a campaign that can never reach zero
// and therefore never completes. Publishing first means a crash mid-way leaves
// the counter unset, which SeedCounterIfMissing rebuilds from the database on
// the next subscribe.
func (d *Dispatcher) Enqueue(campaignID string, messages []Message) error {
	if len(messages) == 0 {
		return nil
	}

	topic := d.ns.DispatchTopic(campaignID)
	for _, m := range messages {
		payload, err := json.Marshal(m)
		if err != nil {
			return fmt.Errorf("failed to marshal campaign dispatch payload: %w", err)
		}
		if err := d.pub.Publish(topic, payload); err != nil {
			return err
		}
	}

	if d.shared != nil {
		d.setCounter(campaignID, len(messages))
	}
	return nil
}

func (d *Dispatcher) setCounter(campaignID string, n int) {
	_ = d.shared.SetString(d.ns.RemainingKey(campaignID), fmt.Sprintf("%d", n), remainingCounterTTL)
}
