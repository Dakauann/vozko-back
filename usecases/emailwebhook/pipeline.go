package emailwebhook_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"vozko/domain/emailapi"
	es "vozko/domain/emailsending"
	wh "vozko/domain/emailwebhook"
	"vozko/domain/notification"
	"vozko/domain/workspace"
)

var _ es.MessageObserver = (*Module)(nil)

func (m *Module) OnMessageEvent(ctx context.Context, msg es.Message, ev es.DeliveryEvent, _ es.EventEffect) error {
	if msg.RunKind != es.RunAPI || ev.IsBot {
		return nil
	}
	found, err := m.d.Statuses.MarkStatus(ctx, msg.ID, emailapi.StatusOf(msg.Status), string(ev.Kind), ev.OccurredAt)
	if err != nil {
		return fmt.Errorf("emailwebhook: recording the %s of message %s: %w", ev.Kind, msg.ID, err)
	}
	if !found {
		m.d.Logf("[email-webhook] %s of API message %s has no log row; nothing to report", ev.Kind, msg.ID)
		return nil
	}
	if _, ok := wh.EventTypeOf(ev); !ok {
		return nil
	}
	payload, err := json.Marshal(FanoutMessage{MessageID: msg.ID, Kind: ev.Kind, OccurredAt: ev.OccurredAt, Link: ev.Link, Bounce: ev.Bounce, Complaint: ev.Complaint})
	if err != nil {
		return err
	}
	if err := m.d.Queue.Publish(TopicFanout, payload); err != nil {
		m.d.Logf("[email-webhook] CRITICAL the %s of message %s was not queued for the customer webhooks: %v", ev.Kind, msg.ID, err)
		return err
	}
	return nil
}

func (m *Module) Fanout(ctx context.Context, msg FanoutMessage) error {
	err := m.fanout(ctx, msg)
	if err == nil {
		return nil
	}
	if msg.Attempt >= fanoutRetries {
		m.d.Logf("[email-webhook] CRITICAL the %s of message %s was dropped after %d attempts: %v", msg.Kind, msg.MessageID, msg.Attempt+1, err)
		return nil
	}
	next := msg
	next.Attempt++
	payload, marshalErr := json.Marshal(next)
	if marshalErr != nil {
		return marshalErr
	}
	if pubErr := m.d.Queue.PublishWithDelay(TopicFanout, payload, time.Duration(5<<msg.Attempt)*time.Second); pubErr != nil {
		m.d.Logf("[email-webhook] CRITICAL the %s of message %s could not be retried: %v (after %v)", msg.Kind, msg.MessageID, pubErr, err)
	}
	return nil
}

func (m *Module) fanout(ctx context.Context, msg FanoutMessage) error {
	ev := msg.event()
	eventType, ok := wh.EventTypeOf(ev)
	if !ok {
		return nil
	}
	entry, err := m.d.Entries.GetByMessageID(ctx, msg.MessageID)
	if errors.Is(err, emailapi.ErrMessageNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	endpoints, err := m.d.Endpoints.Subscribed(ctx, entry.WorkspaceID, eventType)
	if err != nil || len(endpoints) == 0 {
		return err
	}
	now := m.d.Now()
	eventID := wh.EventID(msg.MessageID, msg.Kind, msg.OccurredAt, msg.Link)
	payload, err := wh.BuildPayload(eventID, eventType, *entry, ev, now)
	if err != nil {
		return err
	}
	batch := make([]wh.Delivery, len(endpoints))
	for i, e := range endpoints {
		batch[i] = wh.Delivery{ID: m.d.NewID(), EndpointID: e.ID, WorkspaceID: entry.WorkspaceID, EventID: eventID, EventType: eventType,
			MessageID: entry.ID, Payload: payload, CreatedAt: now}
	}
	inserted, err := m.d.Deliveries.InsertBatch(ctx, batch)
	if err != nil {
		return err
	}
	for _, d := range inserted {
		if err := m.publishDelivery(DeliveryMessage{DeliveryID: d.ID, EndpointID: d.EndpointID, Attempt: 1}); err != nil {
			m.d.Logf("[email-webhook] delivery %s waits in the database for the sweep: %v", d.ID, err)
		}
	}
	return nil
}

func (m *Module) Deliver(ctx context.Context, msg DeliveryMessage) error {
	if msg.EndpointID == "" {
		m.d.Logf("[email-webhook] delivery message %s without its endpoint dropped; the sweep requeues it", msg.DeliveryID)
		return nil
	}
	release, ok, err := m.d.Slots.Acquire(msg.EndpointID)
	if err != nil || !ok {
		payload, marshalErr := json.Marshal(msg)
		if marshalErr != nil {
			return marshalErr
		}
		return m.d.Queue.PublishWithDelay(TopicDelivery, payload, wh.SlotWait)
	}
	defer release()
	target, err := m.d.Deliveries.Target(ctx, msg.DeliveryID)
	if errors.Is(err, wh.ErrDeliveryNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	d, e := target.Delivery, target.Endpoint
	if d.Status != wh.StatusQueued || d.Attempts != msg.Attempt-1 || e.ID != msg.EndpointID {
		return nil
	}
	if !e.Enabled {
		return m.d.Deliveries.Skip(ctx, d.ID, "endpoint paused", m.d.Now())
	}
	claimed, err := m.d.Deliveries.Claim(ctx, d.ID, msg.Attempt, m.d.Now())
	if err != nil || !claimed {
		return err
	}
	d.Attempts = msg.Attempt
	status, duration, postErr := m.post(ctx, e, d)
	now := m.d.Now()
	success := postErr == nil && status >= 200 && status < 300
	outcome := d.AfterAttempt(success, now)
	errText := ""
	switch {
	case postErr != nil:
		errText = postErr.Error()
	case !success:
		errText = "HTTP " + strconv.Itoa(status)
	}
	if err := m.d.Deliveries.Finish(context.WithoutCancel(ctx), wh.Finish{
		ID: d.ID, Outcome: outcome, StatusCode: status, Error: errText, At: now,
		Attempt: wh.Attempt{DeliveryID: d.ID, Attempt: d.Attempts, StatusCode: status, DurationMS: duration.Milliseconds(), Error: errText, NextAttemptAt: outcome.NextAttemptAt, AttemptedAt: now},
	}); err != nil {
		return err
	}
	m.health(context.WithoutCancel(ctx), e, success, now)
	return nil
}

func (m *Module) post(ctx context.Context, e wh.Endpoint, d wh.Delivery) (int, time.Duration, error) {
	secret, err := m.d.Cipher.Decrypt(e.SecretCipher)
	if err != nil {
		return 0, 0, fmt.Errorf("the signing secret is unreadable: %w", err)
	}
	now := m.d.Now()
	headers := map[string]string{
		"Content-Type":        "application/json",
		"User-Agent":          m.d.UserAgent,
		"X-Webhook-Id":        d.EventID,
		"X-Webhook-Attempt":   strconv.Itoa(d.Attempts),
		"X-Webhook-Signature": wh.SignatureHeader(string(secret), now.Unix(), d.Payload),
	}
	callCtx, cancel := context.WithTimeout(ctx, wh.RequestTimeout)
	defer cancel()
	start := time.Now()
	res, err := m.d.Poster.Post(callCtx, e.URL, headers, d.Payload)
	return res.StatusCode, time.Since(start), err
}

func (m *Module) health(ctx context.Context, e wh.Endpoint, success bool, now time.Time) {
	if success {
		if e.FailingSince == nil {
			return
		}
		if err := m.d.Endpoints.MarkSuccess(ctx, e.ID, now); err != nil {
			m.d.Logf("[email-webhook] endpoint %s recovered but the stamp was not saved: %v", e.ID, err)
			return
		}
		m.resolve(ctx, e)
		return
	}
	if err := m.d.Endpoints.MarkFailure(ctx, e.ID, now); err != nil {
		m.d.Logf("[email-webhook] failure of endpoint %s was not stamped: %v", e.ID, err)
		return
	}
	if e.FailingSince == nil {
		return
	}
	if !e.AlertDue(now) {
		return
	}
	m.alert(ctx, e, now)
}

func (m *Module) alert(ctx context.Context, e wh.Endpoint, now time.Time) {
	since := e.FailingSince.In(brasilia()).Format("02/01/2006 15:04")
	_, err := m.d.Alerts.Publish(ctx, notification.Draft{
		WorkspaceID: e.WorkspaceID,
		Category:    notification.CategorySystem,
		Severity:    notification.SeverityWarning,
		Title:       "Um webhook da API de e-mail está falhando",
		Body:        "O endereço " + wh.HostOf(e.URL) + " não confirma o recebimento dos eventos de e-mail desde " + since + ". Os eventos continuam sendo reenviados com intervalos crescentes; o webhook segue ativo até alguém pausá-lo.",
		ActionURL:   WebhooksPath,
		ActionLabel: "Ver webhooks",
		Audience:    notification.Holding(workspace.ResourceEmailMarketing, workspace.ActionConfigure),
		Email:       true,
		EpisodeKey:  wh.AlertKey(e.ID),
		Source:      notification.SourceEmailWebhook,
	})
	if err != nil {
		m.d.Logf("[email-webhook] alert for failing endpoint %s was not published: %v", e.ID, err)
		return
	}
	if err := m.d.Endpoints.MarkAlerted(ctx, e.ID, now); err != nil {
		m.d.Logf("[email-webhook] alert stamp of endpoint %s was not saved: %v", e.ID, err)
	}
}

func (m *Module) resolve(ctx context.Context, e wh.Endpoint) {
	if err := m.d.Alerts.Resolve(ctx, e.WorkspaceID, wh.AlertKey(e.ID)); err != nil {
		m.d.Logf("[email-webhook] resolving the alert of endpoint %s failed: %v", e.ID, err)
	}
}

func brasilia() *time.Location {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		return time.FixedZone("BRT", -3*60*60)
	}
	return loc
}

func (m *Module) Sweep(ctx context.Context) error {
	due, err := m.d.Deliveries.Requeue(ctx, m.d.Now(), wh.SweepBatch)
	if err != nil {
		return err
	}
	for _, d := range due {
		if err := m.publishDelivery(DeliveryMessage{DeliveryID: d.ID, EndpointID: d.EndpointID, Attempt: d.Attempts + 1}); err != nil {
			return fmt.Errorf("emailwebhook: requeueing delivery %s: %w", d.ID, err)
		}
	}
	return nil
}

func (m *Module) Purge(ctx context.Context) error {
	before := m.d.Now().Add(-wh.Retention)
	for _, purge := range []func(context.Context, time.Time, int) (int64, error){m.d.Deliveries.PurgeAttempts, m.d.Deliveries.PurgeDeliveries} {
		for i := 0; i < wh.RetentionMaxChunks; i++ {
			n, err := purge(ctx, before, wh.RetentionChunk)
			if err != nil {
				return err
			}
			if n < wh.RetentionChunk {
				break
			}
		}
	}
	return nil
}
