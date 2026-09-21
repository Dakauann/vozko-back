package campaignqueue

import (
	"encoding/json"
	"fmt"

	"vozko/domain/cache"
	"vozko/domain/campaign"
	"vozko/domain/messaging"
)

type Dispatcher struct {
	pub    messaging.MessageQueuePub
	shared cache.SharedState
	ns     campaign.Namespace
}

func NewDispatcher(pub messaging.MessageQueuePub, sharedState cache.SharedState, ns campaign.Namespace) *Dispatcher {
	return &Dispatcher{pub: pub, shared: sharedState, ns: ns}
}

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
