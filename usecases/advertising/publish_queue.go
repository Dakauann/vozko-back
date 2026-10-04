package advertising

import (
	"encoding/json"
	"errors"
	"time"

	ads "vozko/domain/advertising"
	"vozko/domain/cache"
	"vozko/domain/messaging"
	webhook_usecase "vozko/usecases/webhook"
)

const (
	publishConcurrency = 4
	publishTimeout     = 10 * time.Minute
)

type publishQueue struct {
	pub messaging.MessageQueuePub
}

func NewPublishQueue(pub messaging.MessageQueuePub) ads.PublishQueue {
	return publishQueue{pub: pub}
}

func (q publishQueue) Enqueue(workspaceID, jobID string) error {
	payload, err := json.Marshal(ads.PublishJobMessage{WorkspaceID: workspaceID, JobID: jobID})
	if err != nil {
		return err
	}
	return q.pub.Publish(ads.PublishJobsTopic, payload)
}

func ClassifyPublishFailure(err error) webhook_usecase.Disposition {
	if errors.Is(err, ads.ErrJobNotFound) {
		return webhook_usecase.DispositionDrop
	}
	return webhook_usecase.DispositionRetry
}

func NewPublishConsumer(sub messaging.MessageQueueSub, pub messaging.MessageQueuePub, state cache.SharedState, worker *PublishUseCase) *webhook_usecase.ConsumerRunner[ads.PublishJobMessage] {
	return webhook_usecase.NewConsumerRunner(webhook_usecase.ConsumerConfig[ads.PublishJobMessage]{
		Name:        "ads-publish",
		Topic:       ads.PublishJobsTopic,
		QueueSub:    sub,
		QueuePub:    pub,
		SharedState: state,
		Concurrency: publishConcurrency,
		Timeout:     publishTimeout,
		Handle:      worker.Process,
		Classify:    ClassifyPublishFailure,
	})
}
