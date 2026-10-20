package scheduled_message_usecase

import (
	"fmt"

	"vozko/domain/messaging"
	sm "vozko/domain/scheduled_message"
	"vozko/usecases/shared/delayedfire"
)

func NewConsumeFireUseCase(sub messaging.MessageQueueSub, dispatch sm.DispatchUseCase) (sm.ConsumeFireUseCase, error) {
	if dispatch == nil {
		return nil, fmt.Errorf("scheduled message fire consumer: missing dispatch use case")
	}
	return delayedfire.NewConsumer(sub, sm.TopicFire, "scheduled_message", dispatch.Execute)
}
