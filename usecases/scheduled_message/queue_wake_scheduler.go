package scheduled_message_usecase

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"vozko/domain/messaging"
	sm "vozko/domain/scheduled_message"
)

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
		log.Printf("[scheduled_message] %s fires in %s, publishing immediately", id, delay)
		return s.pub.Publish(sm.TopicFire, payload)
	}

	return s.pub.PublishWithDelay(sm.TopicFire, payload, delay)
}

var _ sm.WakeScheduler = (*queueWakeScheduler)(nil)
