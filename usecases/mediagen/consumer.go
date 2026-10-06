package mediagen_usecase

import (
	"errors"
	"time"

	"vozko/domain/cache"
	"vozko/domain/mediagen"
	"vozko/domain/messaging"
	webhook_usecase "vozko/usecases/webhook"
)

type lane struct {
	name        string
	topic       string
	concurrency int
	timeout     time.Duration
}

var lanes = []lane{
	{name: "media-generation", topic: mediagen.Topic, concurrency: 4, timeout: 4 * time.Minute},
	{name: "media-render", topic: mediagen.RenderTopic, concurrency: 2, timeout: 6 * time.Minute},
}

func Classify(err error) webhook_usecase.Disposition {
	if errors.Is(err, mediagen.ErrJobNotFound) {
		return webhook_usecase.DispositionDrop
	}
	return webhook_usecase.DispositionRetry
}

func NewConsumers(sub messaging.MessageQueueSub, pub messaging.MessageQueuePub, state cache.SharedState, svc *Service) []*webhook_usecase.ConsumerRunner[mediagen.QueueMessage] {
	runners := make([]*webhook_usecase.ConsumerRunner[mediagen.QueueMessage], 0, len(lanes))
	for _, l := range lanes {
		runners = append(runners, webhook_usecase.NewConsumerRunner(webhook_usecase.ConsumerConfig[mediagen.QueueMessage]{
			Name:        l.name,
			Topic:       l.topic,
			QueueSub:    sub,
			QueuePub:    pub,
			SharedState: state,
			Concurrency: l.concurrency,
			Timeout:     l.timeout,
			Handle:      svc.Process,
			Classify:    Classify,
		}))
	}
	return runners
}
