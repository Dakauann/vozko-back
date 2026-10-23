package emailwiring

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"gorm.io/gorm"

	"vozko/domain/cache"
	wh "vozko/domain/emailwebhook"
	"vozko/domain/media"
	"vozko/domain/messaging"
	"vozko/domain/notification"
	infracache "vozko/infra/cache"
	emailapi_repository "vozko/infra/repositories/emailapi"
	emailcampaign_repository "vozko/infra/repositories/emailcampaign"
	emailsending_repository "vozko/infra/repositories/emailsending"
	emailwebhook_repository "vozko/infra/repositories/emailwebhook"
	emailapi_usecase "vozko/usecases/emailapi"
	esuc "vozko/usecases/emailsending"
	emailwebhook_usecase "vozko/usecases/emailwebhook"
	webhook_usecase "vozko/usecases/webhook"
)

const (
	fanoutConsumer      = "email-api-webhook-fanout"
	deliveryConsumer    = "email-api-webhook-delivery"
	fanoutConcurrency   = 10
	deliveryConcurrency = 20
	slotPrefix          = "email:webhook:slot:"
)

type SlotState interface {
	cache.SharedState
	DelIfValue(key, value string) (bool, error)
}

type EmailAPIAdapters struct {
	Sending   *esuc.Module
	Billing   emailapi_usecase.Billing
	Files     media.RemoteFiles
	QueuePub  messaging.MessageQueuePub
	Shared    SlotState
	Alerts    notification.EpisodePublisher
	Cipher    wh.Cipher
	Hosts     wh.HostChecker
	Client    *http.Client
	UserAgent string
	Now       func() time.Time
}

type EmailAPI struct {
	Sends    *emailapi_repository.Sends
	Service  *emailapi_usecase.Service
	Webhooks *emailwebhook_usecase.Module
}

func NewEmailAPI(db *gorm.DB, a EmailAPIAdapters) (*EmailAPI, error) {
	if db == nil || a.Sending == nil || a.Sending.Gateway == nil || a.Sending.Events == nil {
		return nil, fmt.Errorf("%w: the database and email sending must exist before the email API", ErrIncomplete)
	}
	if a.Client == nil || a.Shared == nil {
		return nil, fmt.Errorf("%w: the email API needs the guarded webhook client and the shared state", ErrIncomplete)
	}
	sends := emailapi_repository.NewSends(db)
	logs := emailapi_repository.NewLog(db)
	service, err := emailapi_usecase.New(emailapi_usecase.Deps{
		Sends:        sends,
		Log:          logs,
		Runs:         emailsending_repository.NewRunMessages(db),
		Gateway:      a.Sending.Gateway,
		Billing:      a.Billing,
		Designs:      emailcampaign_repository.NewDesignRepository(db),
		Senders:      emailsending_repository.NewSenderRepository(db),
		Domains:      emailsending_repository.NewDomainRepository(db),
		Suppressions: emailsending_repository.NewSuppressionRepository(db),
		Files:        a.Files,
		Now:          a.Now,
	})
	if err != nil {
		return nil, err
	}
	webhooks, err := emailwebhook_usecase.New(emailwebhook_usecase.Deps{
		Endpoints:  emailwebhook_repository.NewEndpoints(db),
		Deliveries: emailwebhook_repository.NewDeliveries(db),
		Statuses:   sends,
		Entries:    logs,
		Cipher:     a.Cipher,
		Hosts:      a.Hosts,
		Poster:     WebhookPoster{Client: a.Client},
		Slots:      infracache.NewSlots(a.Shared, slotPrefix, wh.MaxInFlightPerEndpoint, wh.SlotTTL),
		Queue:      a.QueuePub,
		Alerts:     a.Alerts,
		UserAgent:  a.UserAgent,
		Now:        a.Now,
	})
	if err != nil {
		return nil, err
	}
	a.Sending.Events.AddObserver(webhooks)
	return &EmailAPI{Sends: sends, Service: service, Webhooks: webhooks}, nil
}

type Starter interface {
	Start() error
}

func (e *EmailAPI) Consumers(sub messaging.MessageQueueSub, pub messaging.MessageQueuePub, shared cache.SharedState) []Starter {
	drop := func(error) webhook_usecase.Disposition { return webhook_usecase.DispositionDrop }
	return []Starter{
		webhook_usecase.NewConsumerRunner(webhook_usecase.ConsumerConfig[emailwebhook_usecase.FanoutMessage]{
			Name: fanoutConsumer, Topic: emailwebhook_usecase.TopicFanout, QueueSub: sub, QueuePub: pub, SharedState: shared,
			Concurrency: fanoutConcurrency, Timeout: time.Minute, Classify: drop,
			Handle: func(ctx context.Context, m *emailwebhook_usecase.FanoutMessage) error {
				return e.Webhooks.Fanout(ctx, *m)
			},
		}),
		webhook_usecase.NewConsumerRunner(webhook_usecase.ConsumerConfig[emailwebhook_usecase.DeliveryMessage]{
			Name: deliveryConsumer, Topic: emailwebhook_usecase.TopicDelivery, QueueSub: sub, QueuePub: pub, SharedState: shared,
			Concurrency: deliveryConcurrency, Timeout: time.Minute, Classify: drop,
			Handle: func(ctx context.Context, m *emailwebhook_usecase.DeliveryMessage) error {
				return e.Webhooks.Deliver(ctx, *m)
			},
		}),
	}
}

type WebhookPoster struct {
	Client *http.Client
}

const maxWebhookResponseBytes = 64 << 10

func (p WebhookPoster) Post(ctx context.Context, url string, headers map[string]string, body []byte) (wh.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return wh.Response{}, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := *p.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		return wh.Response{}, err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, maxWebhookResponseBytes))
	return wh.Response{StatusCode: res.StatusCode}, nil
}
