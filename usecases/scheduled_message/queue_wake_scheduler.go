package scheduled_message_usecase

import (
	"fmt"

	"vozko/domain/messaging"
	sm "vozko/domain/scheduled_message"
	"vozko/usecases/shared/delayedfire"
)

func NewQueueWakeScheduler(pub messaging.MessageQueuePub) (sm.WakeScheduler, error) {
	scheduler, err := delayedfire.NewScheduler(pub, sm.TopicFire)
	if err != nil {
		return nil, fmt.Errorf("scheduled message wake scheduler: %w", err)
	}
	return scheduler, nil
}
