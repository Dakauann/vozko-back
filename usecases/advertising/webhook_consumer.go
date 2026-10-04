package advertising

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	ads "vozko/domain/advertising"
	"vozko/domain/cache"
	"vozko/domain/messaging"
	mm "vozko/domain/metamessaging"
	"vozko/domain/webhook"
	webhook_usecase "vozko/usecases/webhook"
)

const (
	leadgenConcurrency   = 5
	adAccountConcurrency = 3
	leadgenField         = "leadgen"
)

var adsDialect = mm.Dialect{Prefix: "ads"}

type leadgenHandler interface {
	HandleLeadgen(ctx context.Context, event ads.LeadgenEvent) error
}

type accountChangeHandler interface {
	Handle(ctx context.Context, changes []ads.AdAccountChange) error
}

type WebhookConsumerDeps struct {
	QueueSub    messaging.MessageQueueSub
	QueuePub    messaging.MessageQueuePub
	SharedState cache.SharedState
	Durable     webhook.ProcessedEventRepository
	Leads       leadgenHandler
	Accounts    accountChangeHandler
}

type WebhookConsumer struct {
	runners []*webhook_usecase.ConsumerRunner[mm.EntryEnvelope]
}

func NewWebhookConsumer(d WebhookConsumerDeps) *WebhookConsumer {
	build := func(topic, name string, concurrency int, handle func(context.Context, *mm.EntryEnvelope) error) *webhook_usecase.ConsumerRunner[mm.EntryEnvelope] {
		return webhook_usecase.NewConsumerRunner(webhook_usecase.ConsumerConfig[mm.EntryEnvelope]{
			Name:        name,
			Topic:       topic,
			QueueSub:    d.QueueSub,
			QueuePub:    d.QueuePub,
			SharedState: d.SharedState,
			Durable:     d.Durable,
			Concurrency: concurrency,
			DedupKey:    adsDialect.EntryDedupKey,
			Handle:      handle,
			Classify:    classifyWebhookFailure,
		})
	}
	return &WebhookConsumer{runners: []*webhook_usecase.ConsumerRunner[mm.EntryEnvelope]{
		build(webhook.TopicFacebookLeadgen, "ads-leadgen-webhook", leadgenConcurrency, leadgenEntryHandler(d.Leads)),
		build(webhook.TopicMetaAdAccount, "ads-account-webhook", adAccountConcurrency, accountEntryHandler(d.Accounts)),
	}}
}

func (c *WebhookConsumer) Start() error {
	for _, r := range c.runners {
		if err := r.Start(); err != nil {
			return err
		}
	}
	return nil
}

func leadgenEntryHandler(leads leadgenHandler) func(context.Context, *mm.EntryEnvelope) error {
	return func(ctx context.Context, env *mm.EntryEnvelope) error {
		events, err := leadgenEvents(env)
		if err != nil {
			return err
		}
		var failures []error
		for _, event := range events {
			if err := leads.HandleLeadgen(ctx, event); err != nil {
				failures = append(failures, err)
			}
		}
		return errors.Join(failures...)
	}
}

func accountEntryHandler(accounts accountChangeHandler) func(context.Context, *mm.EntryEnvelope) error {
	return func(ctx context.Context, env *mm.EntryEnvelope) error {
		changes, err := accountChanges(env)
		if err != nil {
			return err
		}
		if len(changes) == 0 {
			return nil
		}
		return accounts.Handle(ctx, changes)
	}
}

type leadgenValue struct {
	LeadgenID   json.Number `json:"leadgen_id"`
	FormID      json.Number `json:"form_id"`
	AdID        json.Number `json:"ad_id"`
	PageID      json.Number `json:"page_id"`
	CreatedTime int64       `json:"created_time"`
}

func leadgenEvents(env *mm.EntryEnvelope) ([]ads.LeadgenEvent, error) {
	if env == nil || env.Entry == nil {
		return nil, mm.ErrInvalidWebhookPayload
	}
	var events []ads.LeadgenEvent
	for _, change := range env.Entry.Changes {
		if change == nil || change.Field != leadgenField {
			continue
		}
		var v leadgenValue
		if err := json.Unmarshal(change.Value, &v); err != nil || v.LeadgenID == "" || v.FormID == "" {
			return nil, mm.ErrInvalidWebhookPayload
		}
		pageID := v.PageID.String()
		if pageID == "" {
			pageID = env.Entry.ID
		}
		events = append(events, ads.LeadgenEvent{
			LeadgenID: v.LeadgenID.String(),
			FormID:    v.FormID.String(),
			AdID:      v.AdID.String(),
			PageID:    pageID,
			CreatedAt: mm.UnixTime(v.CreatedTime),
		})
	}
	return events, nil
}

type adObjectValue struct {
	ID         json.Number `json:"id"`
	Level      string      `json:"level"`
	ObjectID   json.Number `json:"object_id"`
	ObjectType string      `json:"object_type"`
}

func (v adObjectValue) change(accountMetaID, field string) ads.AdAccountChange {
	change := ads.AdAccountChange{AccountMetaID: accountMetaID, Field: field}
	id, level := v.ID.String(), v.Level
	if v.ObjectID != "" {
		id, level = v.ObjectID.String(), v.ObjectType
	}
	if id == "" {
		return change
	}
	ref, ok := ads.ObjectRefOf(id, level)
	if !ok {
		change.Unresolved = true
		return change
	}
	change.Objects = []ads.ObjectRef{ref}
	return change
}

func accountChanges(env *mm.EntryEnvelope) ([]ads.AdAccountChange, error) {
	if env == nil || env.Entry == nil || strings.TrimSpace(env.Entry.ID) == "" {
		return nil, mm.ErrInvalidWebhookPayload
	}
	var changes []ads.AdAccountChange
	for _, change := range env.Entry.Changes {
		if change == nil || change.Field == "" {
			continue
		}
		parsed := ads.AdAccountChange{AccountMetaID: env.Entry.ID, Field: change.Field}
		if parsed.Kind() == ads.AccountObjectsChanged && len(change.Value) > 0 {
			var v adObjectValue
			if err := json.Unmarshal(change.Value, &v); err != nil {
				return nil, mm.ErrInvalidWebhookPayload
			}
			parsed = v.change(env.Entry.ID, change.Field)
		}
		changes = append(changes, parsed)
	}
	return changes, nil
}

func classifyWebhookFailure(err error) webhook_usecase.Disposition {
	if err == nil || errors.Is(err, mm.ErrInvalidWebhookPayload) {
		return webhook_usecase.DispositionDrop
	}
	switch ads.Classify(err) {
	case ads.FailureRetryable, ads.FailureUnknown:
		return webhook_usecase.DispositionRetry
	case ads.FailureReauth, ads.FailurePermission:
		return webhook_usecase.DispositionDrop
	}
	return webhook_usecase.DispositionDeadLetter
}
