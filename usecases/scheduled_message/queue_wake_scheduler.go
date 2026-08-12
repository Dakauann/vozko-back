package scheduled_message_usecase

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"vozko/domain/messaging"
	sm "vozko/domain/scheduled_message"
)

// immediateFireThreshold is the delay below which the message is published for
// immediate delivery instead of parked on the delay queue.
//
// The delay queue works by TTL-expiring a message onto the real queue, and for
// a delay of a few seconds that machinery costs more than it saves. Mirrors the
// workflow wake queue's threshold, which exists for the same reason.
const immediateFireThreshold = 10 * time.Second

type queueWakeScheduler struct {
	pub messaging.MessageQueuePub
}

func NewQueueWakeScheduler(pub messaging.MessageQueuePub) (sm.WakeScheduler, error) {
	if pub == nil {
		return nil, fmt.Errorf("scheduled message wake scheduler: missing queue publisher")
	}
	return &queueWakeScheduler{pub: pub}, nil
}

func (s *queueWakeScheduler) ScheduleFire(id string, fireAt time.Time) error {
	payload, err := json.Marshal(sm.FireMessage{ID: id, FireAt: fireAt.UTC().UnixMilli()})
	if err != nil {
		return err
	}

	delay := time.Until(fireAt)
	if delay <= immediateFireThreshold {
		// Includes a fireAt already in the past, which happens when a schedule
		// is created against a clock that has since moved on. Publishing it now
		// is right: the dispatcher re-checks the window, and the row's own
		// scheduled_at is what the UI reports.
		log.Printf("[scheduled_message] %s fires in %s, publishing immediately", id, delay)
		return s.pub.Publish(sm.TopicFire, payload)
	}

	return s.pub.PublishWithDelay(sm.TopicFire, payload, delay)
}

var _ sm.WakeScheduler = (*queueWakeScheduler)(nil)
