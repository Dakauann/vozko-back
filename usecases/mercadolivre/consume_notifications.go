package mercadolivre

import (
	"errors"
	"log"

	"vozko/domain/cache"
	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/messaging"
	"vozko/domain/webhook"
	webhook_usecase "vozko/usecases/webhook"
)

type consumerTopic struct {
	topic       string
	name        string
	concurrency int
}

func consumerTopics() []consumerTopic {
	return []consumerTopic{
		{webhook.TopicMercadoLivreMessage, "mercadolivre-message-notifications", 20},
		{webhook.TopicMercadoLivreQuestion, "mercadolivre-question-notifications", 10},
		{webhook.TopicMercadoLivreOrder, "mercadolivre-order-notifications", 10},
	}
}

type ConsumeNotificationsDeps struct {
	QueueSub    messaging.MessageQueueSub
	QueuePub    messaging.MessageQueuePub
	SharedState cache.SharedState
	Durable     webhook.ProcessedEventRepository
	Dispatcher  *NotificationDispatcher
	Errors      mldomain.ErrorClassifier
	Metrics     Metrics
}

type ConsumeNotifications struct {
	runners []interface{ Start() error }
}

func NewConsumeNotifications(d ConsumeNotificationsDeps) *ConsumeNotifications {
	if d.QueueSub == nil || d.QueuePub == nil || d.SharedState == nil || d.Durable == nil || d.Dispatcher == nil || d.Errors == nil || d.Metrics == nil {
		panic("mercadolivre: the notification consumers require the queue, shared state, durable dedup, the dispatcher, an error classifier and metrics")
	}
	classify := ClassifyNotificationFailure(d.Errors, d.Metrics)
	uc := &ConsumeNotifications{}
	for _, t := range consumerTopics() {
		uc.runners = append(uc.runners, webhook_usecase.NewConsumerRunner(webhook_usecase.ConsumerConfig[mldomain.Job]{
			Name:        t.name,
			Topic:       t.topic,
			QueueSub:    d.QueueSub,
			QueuePub:    d.QueuePub,
			SharedState: d.SharedState,
			Durable:     d.Durable,
			Concurrency: t.concurrency,
			DedupKey:    func(j *mldomain.Job) string { return j.IdempotencyKey() },
			Handle:      d.Dispatcher.Execute,
			Classify:    classify,
		}))
	}
	return uc
}

func (uc *ConsumeNotifications) Start() error {
	for _, r := range uc.runners {
		if err := r.Start(); err != nil {
			return err
		}
	}
	return nil
}

func ClassifyNotificationFailure(errs mldomain.ErrorClassifier, metrics Metrics) func(error) webhook_usecase.Disposition {
	return func(err error) webhook_usecase.Disposition {
		disposition, outcome := classifyNotification(errs, err)
		metrics.AddMercadoLivreEvent("consume", outcome)
		if disposition != webhook_usecase.DispositionRetry {
			log.Printf("[mercadolivre][notify] %s: %v", outcome, err)
		}
		return disposition
	}
}

func classifyNotification(errs mldomain.ErrorClassifier, err error) (webhook_usecase.Disposition, string) {
	switch {
	case errors.Is(err, ErrUnknownAccount), errors.Is(err, ErrAccountUnavailable):
		return webhook_usecase.DispositionDrop, "dropped_account"
	case errors.Is(err, ErrResourceGone):
		return webhook_usecase.DispositionDrop, "dropped_gone"
	case errors.Is(err, ErrJobKindNotHandled),
		errors.Is(err, mldomain.ErrUnknownJobKind),
		errors.Is(err, mldomain.ErrInvalidResource),
		errors.Is(err, mldomain.ErrInvalidNotification),
		errors.Is(err, mldomain.ErrPackUnknown),
		errors.Is(err, mldomain.ErrBuyerUnknown),
		errs.IsDecode(err):
		return webhook_usecase.DispositionDeadLetter, "dead_letter"
	case errs.IsAppBlocked(err):
		return webhook_usecase.DispositionDeadLetter, "dead_letter_app_blocked"
	}
	return webhook_usecase.DispositionRetry, "retry"
}
