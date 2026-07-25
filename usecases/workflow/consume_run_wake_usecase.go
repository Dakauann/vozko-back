package workflow_usecase

import (
	"encoding/json"
	"log"
	"time"

	"vozko/domain/messaging"
	"vozko/domain/workflow"
)

type consumeRunWakeUseCase struct {
	queueSub     messaging.MessageQueueSub
	queuePub     messaging.MessageQueuePub
	workflowRepo workflow.WorkflowRepository
	runRepo      workflow.WorkflowRunRepository
	engine       *RunEngine
}

func NewConsumeRunWakeUseCase(
	queueSub messaging.MessageQueueSub,
	queuePub messaging.MessageQueuePub,
	workflowRepo workflow.WorkflowRepository,
	runRepo workflow.WorkflowRunRepository,
	engine *RunEngine,
) workflow.ConsumeRunWakeUseCase {
	return &consumeRunWakeUseCase{
		queueSub:     queueSub,
		queuePub:     queuePub,
		workflowRepo: workflowRepo,
		runRepo:      runRepo,
		engine:       engine,
	}
}

func (uc *consumeRunWakeUseCase) Start() error {
	if uc == nil || uc.queueSub == nil {
		log.Printf("[workflow][wake] consumer disabled: queue subscriber unavailable")
		return nil
	}

	log.Printf("[workflow][wake] consumer subscribing to topic=%s", workflow.TopicRunWake)

	return uc.queueSub.Subscribe(workflow.TopicRunWake, func(message []byte, ack messaging.MessageAck) {
		if ack == nil {
			log.Printf("[workflow][wake] received message without ack handler")
			return
		}

		var wake workflow.RunWakeMessage
		if err := json.Unmarshal(message, &wake); err != nil {
			log.Printf("[workflow][wake] invalid message payload: %v", err)
			_ = ack.Nack(false)
			return
		}
		if wake.RunID == "" {
			log.Printf("[workflow][wake] invalid message: empty run_id")
			_ = ack.Nack(false)
			return
		}

		log.Printf("[workflow][wake] received wake run=%s wake_at=%d", wake.RunID, wake.WakeAt)

		run, err := uc.runRepo.FindByID(wake.RunID)
		if err != nil {
			log.Printf("[workflow][wake] failed to load run %s: %v", wake.RunID, err)
			_ = ack.Nack(true)
			return
		}
		if run == nil {
			_ = ack.Ack()
			return
		}
		if run.Status != workflow.RunStatusWaiting || run.WakeAt == nil {
			log.Printf("[workflow][wake] ignoring run=%s status=%s wake_at_set=%v", run.ID, run.Status, run.WakeAt != nil)
			_ = ack.Ack()
			return
		}
		if wake.WakeAt > 0 && *run.WakeAt != wake.WakeAt {

			log.Printf("[workflow][wake] stale wake ignored run=%s current_wake_at=%d msg_wake_at=%d", run.ID, *run.WakeAt, wake.WakeAt)
			_ = ack.Ack()
			return
		}

		nowMs := time.Now().UTC().UnixMilli()
		if *run.WakeAt > nowMs {

			remaining := *run.WakeAt - nowMs
			if remaining <= immediateWakeThresholdMs {
				log.Printf("[workflow][wake] early short delivery run=%s remaining_ms=%d, sleeping in consumer", run.ID, remaining)
				time.Sleep(time.Duration(remaining) * time.Millisecond)
			} else {
				log.Printf("[workflow][wake] early delivery run=%s remaining_ms=%d, requeueing", run.ID, remaining)
				payload, marshalErr := json.Marshal(workflow.RunWakeMessage{RunID: run.ID, WakeAt: *run.WakeAt})
				if marshalErr != nil {
					_ = ack.Nack(false)
					return
				}
				if requeueErr := uc.publishDelay(payload, remaining); requeueErr != nil {
					log.Printf("[workflow][wake] failed to requeue early wake run=%s: %v", run.ID, requeueErr)
					_ = ack.Nack(true)
					return
				}
				_ = ack.Ack()
				return
			}
		}

		if !uc.engine.TryLockRun(run.ID) {
			log.Printf("[workflow][wake] skipping run=%s — already locked", run.ID)
			_ = ack.Nack(true)
			return
		}

		if err := resumeRunFromCurrent(uc.workflowRepo, uc.runRepo, uc.engine, run); err != nil {
			log.Printf("[workflow][wake] failed to resume run %s: %v", run.ID, err)
			uc.engine.UnlockRun(run.ID)
			_ = ack.Nack(true)
			return
		}

		uc.engine.UnlockRun(run.ID)
		log.Printf("[workflow][wake] resumed run=%s successfully", run.ID)

		_ = ack.Ack()
	})
}

func (uc *consumeRunWakeUseCase) publishDelay(payload []byte, delayMs int64) error {
	if delayMs < 1 {
		delayMs = 1
	}
	if uc.queuePub != nil {
		return uc.queuePub.PublishWithDelay(workflow.TopicRunWake, payload, time.Duration(delayMs)*time.Millisecond)
	}

	return nil
}
