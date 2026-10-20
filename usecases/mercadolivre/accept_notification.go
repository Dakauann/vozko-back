package mercadolivre

import (
	"encoding/json"
	"errors"
	"fmt"

	mldomain "vozko/domain/mercadolivre"
	"vozko/pkg/webhookauth"
)

var (
	ErrNotificationRefused = errors.New("mercadolivre: notification refused")
	ErrQueueUnavailable    = errors.New("mercadolivre: the notification queue is unavailable")
	ErrNotificationToken   = errors.New("mercadolivre: the notification path token does not match")
)

type JobPublisher struct {
	queue QueuePublisher
}

func NewJobPublisher(queue QueuePublisher) *JobPublisher {
	if queue == nil {
		panic("mercadolivre: the job publisher requires the queue")
	}
	return &JobPublisher{queue: queue}
}

func (p *JobPublisher) Publish(job mldomain.Job) error {
	topic, err := mldomain.TopicForJob(job.Kind)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}
	if err := p.queue.Publish(topic, payload); err != nil {
		return fmt.Errorf("%w: %v", ErrQueueUnavailable, err)
	}
	return nil
}

type AcceptNotification struct {
	appID     string
	token     string
	publisher *JobPublisher
	metrics   Metrics
}

func NewAcceptNotification(appID, token string, publisher *JobPublisher, metrics Metrics) *AcceptNotification {
	if mldomain.ValidateAppID(appID) != nil || mldomain.ValidateNotificationToken(token) != nil || publisher == nil || metrics == nil {
		panic("mercadolivre: accepting notifications requires the numeric app id, a strong path token, the job publisher and metrics")
	}
	return &AcceptNotification{appID: appID, token: token, publisher: publisher, metrics: metrics}
}

func (a *AcceptNotification) AcceptWithToken(token string, raw []byte) error {
	if !webhookauth.ConstantTimeEqual(token, a.token) {
		a.metrics.AddMercadoLivreEvent("notify", "refused_token")
		return ErrNotificationToken
	}
	return a.Accept(raw)
}

func (a *AcceptNotification) Accept(raw []byte) error {
	n, err := mldomain.Decode(raw)
	if err != nil {
		a.metrics.AddMercadoLivreEvent("notify", "refused_decode")
		return fmt.Errorf("%w: %v", ErrNotificationRefused, err)
	}
	return a.AcceptDecoded(n)
}

func (a *AcceptNotification) AcceptDecoded(n mldomain.Notification) error {
	job, err := mldomain.Normalize(n, a.appID)
	if err != nil {
		a.metrics.AddMercadoLivreEvent("notify", refusalOutcome(err))
		return fmt.Errorf("%w: %v", ErrNotificationRefused, err)
	}
	if err := a.publisher.Publish(job); err != nil {
		a.metrics.AddMercadoLivreEvent("notify", "queue_unavailable")
		return err
	}
	a.metrics.AddMercadoLivreEvent("notify", "accepted")
	return nil
}

func refusalOutcome(err error) string {
	switch {
	case errors.Is(err, mldomain.ErrWrongApplication):
		return "refused_application"
	case errors.Is(err, mldomain.ErrUnknownTopic):
		return "refused_unknown_topic"
	case errors.Is(err, mldomain.ErrInvalidResource):
		return "refused_resource"
	case errors.Is(err, mldomain.ErrUserIDInvalid):
		return "refused_seller"
	case errors.Is(err, mldomain.ErrUnknownAction):
		return "refused_action"
	case errors.Is(err, mldomain.ErrNotificationIdentity):
		return "refused_identity"
	}
	return "refused_other"
}
