package workflow_usecase

import (
	"encoding/json"
	"log"
	"time"

	"vozko/domain/messaging"
	"vozko/domain/workflow"
)

type queueWakeScheduler struct {
	pub messaging.MessageQueuePub
}

const immediateWakeThresholdMs int64 = 10_000

func NewQueueWakeScheduler(pub messaging.MessageQueuePub) workflow.WakeScheduler {
	return &queueWakeScheduler{pub: pub}
}

func (s *queueWakeScheduler) ScheduleRunWake(runID string, wakeAt int64) error {
	if s == nil || s.pub == nil || runID == "" {
		log.Printf("[workflow][wake] scheduler unavailable, skipping delayed wake run=%s", runID)
		return nil
	}

	msg := workflow.RunWakeMessage{
		RunID:  runID,
		WakeAt: wakeAt,
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	delayMs := wakeAt - time.Now().UTC().UnixMilli()
	if delayMs < 1 {
		delayMs = 1
	}

	log.Printf("[workflow][wake] enqueue run=%s wake_at=%d delay_ms=%d", runID, wakeAt, delayMs)
	if delayMs <= immediateWakeThresholdMs {
		log.Printf("[workflow][wake] short delay detected run=%s delay_ms=%d, publishing immediate wake", runID, delayMs)
		return s.pub.Publish(workflow.TopicRunWake, payload)
	}

	return s.pub.PublishWithDelay(workflow.TopicRunWake, payload, time.Duration(delayMs)*time.Millisecond)
}
