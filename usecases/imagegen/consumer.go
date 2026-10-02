package imagegen_usecase

import (
	"errors"
	"time"

	"vozko/domain/cache"
	"vozko/domain/imagegen"
	"vozko/domain/messaging"
	webhook_usecase "vozko/usecases/webhook"
)

const (
	consumerConcurrency = 4
	consumerTimeout     = 4 * time.Minute
)

func Classify(err error) webhook_usecase.Disposition {
	if errors.Is(err, imagegen.ErrJobNotFound) {
		return webhook_usecase.DispositionDrop
	}
	return webhook_usecase.DispositionRetry
}

func NewConsumer(sub messaging.MessageQueueSub, pub messaging.MessageQueuePub, state cache.SharedState, svc *Service) *webhook_usecase.ConsumerRunner[imagegen.QueueMessage] {
	return webhook_usecase.NewConsumerRunner(webhook_usecase.ConsumerConfig[imagegen.QueueMessage]{
		Name:        "image-generation",
		Topic:       imagegen.Topic,
		QueueSub:    sub,
		QueuePub:    pub,
		SharedState: state,
		Concurrency: consumerConcurrency,
		Timeout:     consumerTimeout,
		Handle:      svc.Process,
		Classify:    Classify,
	})
}
