package scheduled_message_usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"vozko/domain/messaging"
	sm "vozko/domain/scheduled_message"
)

type consumeFireUseCase struct {
	sub      messaging.MessageQueueSub
	dispatch sm.DispatchUseCase
}

func NewConsumeFireUseCase(sub messaging.MessageQueueSub, dispatch sm.DispatchUseCase) (sm.ConsumeFireUseCase, error) {
	missing := []string{}
	if sub == nil {
		missing = append(missing, "queue subscriber")
	}
	if dispatch == nil {
		missing = append(missing, "dispatch use case")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("scheduled message fire consumer: missing %s", strings.Join(missing, ", "))
	}
	return &consumeFireUseCase{sub: sub, dispatch: dispatch}, nil
}

func (uc *consumeFireUseCase) Start() error {
	return uc.sub.Subscribe(sm.TopicFire, uc.handle)
}

// handle dispatches one fire signal.
//
// Every path acks. A redelivery cannot produce a second message — the claim
// admits one caller — but it also cannot achieve anything the first attempt did
// not, because a failed dispatch is recorded as failed and shown to the
// operator rather than left for the queue to retry. Nacking would only spin.
func (uc *consumeFireUseCase) handle(payload []byte, ack messaging.MessageAck) {
	defer func() {
		if err := ack.Ack(); err != nil {
			log.Printf("[scheduled_message] could not ack a fire message: %v", err)
		}
	}()

	var msg sm.FireMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		log.Printf("[scheduled_message] discarding an unparseable fire message: %v", err)
		return
	}
	if strings.TrimSpace(msg.ID) == "" {
		log.Printf("[scheduled_message] discarding a fire message with no id")
		return
	}

	if err := uc.dispatch.Execute(context.Background(), msg.ID); err != nil {
		log.Printf("[scheduled_message] dispatch of %s failed: %v", msg.ID, err)
	}
}

var _ sm.ConsumeFireUseCase = (*consumeFireUseCase)(nil)
