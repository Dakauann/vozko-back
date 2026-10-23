package emailwebhook_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/emailapi"
	es "vozko/domain/emailsending"
	wh "vozko/domain/emailwebhook"
	"vozko/domain/notification"
	"vozko/domain/workspace"
)

func (w *world) endpoint(t *testing.T, events ...string) WithSecret {
	t.Helper()
	created, err := w.module.Create(context.Background(), CreateInput{WorkspaceID: "ws-1", UserID: "u-1", URL: "https://hooks.acme.com/email", Events: events, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func (w *world) apiMessage(id string) {
	w.statuses.known[id] = true
	w.entries[id] = emailapi.LogEntry{
		Message: emailapi.Message{ID: "row-" + id, SendID: "send-1", WorkspaceID: "ws-1", Address: "ana@example.com", Role: emailapi.RoleTo, MessageID: id},
		Send:    emailapi.Send{ID: "send-1", IdempotencyKey: "k-1", From: "no-reply@acme.com", Subject: "Oi", Tags: map[string]string{"kind": "reset"}},
	}
}

func (w *world) event(t *testing.T, id string, kind es.EventKind) {
	t.Helper()
	err := w.module.OnMessageEvent(context.Background(), es.Message{ID: id, WorkspaceID: "ws-1", RunKind: es.RunAPI, Status: es.MessageDelivered},
		es.DeliveryEvent{Kind: kind, OccurredAt: w.now}, es.EventEffect{Changed: true})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCreateValidatesTheURLAndShowsTheSecretOnce(t *testing.T) {
	w := newWorld(t)
	created := w.endpoint(t, "email.delivered", "email.bounced")
	stored := w.endpoints.endpoints[created.Endpoint.ID]
	if !strings.HasPrefix(created.Secret, wh.SecretPrefix) || string(stored.SecretCipher) != "sealed:"+created.Secret || stored.SecretHint != wh.Hint(created.Secret) {
		t.Fatalf("stored = %+v", stored)
	}
	for _, c := range []struct {
		url  string
		want error
	}{
		{"http://hooks.acme.com", wh.ErrURLInvalid},
		{"https://internal.example.com/x", wh.ErrURLBlocked},
	} {
		if _, err := w.module.Create(context.Background(), CreateInput{WorkspaceID: "ws-1", URL: c.url, Events: []string{"email.sent"}}); !errors.Is(err, c.want) {
			t.Fatalf("%s = %v", c.url, err)
		}
	}
	if _, err := w.module.Create(context.Background(), CreateInput{WorkspaceID: "ws-1", URL: "https://a.com", Events: []string{"x"}}); !errors.Is(err, wh.ErrEventsInvalid) {
		t.Fatalf("events = %v", err)
	}
	rotated, err := w.module.RotateSecret(context.Background(), "ws-1", created.Endpoint.ID)
	if err != nil || rotated.Secret == created.Secret || string(w.endpoints.endpoints[created.Endpoint.ID].SecretCipher) != "sealed:"+rotated.Secret {
		t.Fatalf("rotate = %+v %v", rotated, err)
	}
	blocked := "https://internal.example.com/x"
	if _, err := w.module.Update(context.Background(), UpdateInput{WorkspaceID: "ws-1", ID: created.Endpoint.ID, URL: &blocked}); !errors.Is(err, wh.ErrURLBlocked) {
		t.Fatalf("update to a private host = %v", err)
	}
}

func TestOnlyAPIMessagesReachTheCustomerWebhooks(t *testing.T) {
	w := newWorld(t)
	for _, kind := range []es.RunKind{es.RunCampaign, es.RunTriggered, es.RunReply, es.RunTest} {
		err := w.module.OnMessageEvent(context.Background(), es.Message{ID: "m", RunKind: kind}, es.DeliveryEvent{Kind: es.EventDelivery}, es.EventEffect{Changed: true})
		if err != nil || len(w.statuses.marked) != 0 || len(w.queue.msgs) != 0 {
			t.Fatalf("%s touched the API pipeline", kind)
		}
	}
	_ = w.module.OnMessageEvent(context.Background(), es.Message{ID: "m", RunKind: es.RunAPI}, es.DeliveryEvent{Kind: es.EventOpen, IsBot: true}, es.EventEffect{Changed: true})
	if len(w.statuses.marked) != 0 || len(w.queue.msgs) != 0 {
		t.Fatal("bot opens are not customer events")
	}
}

func TestTheObserverDoesOneWriteAndOnePublishAndNeverFansOutInline(t *testing.T) {
	w := newWorld(t)
	w.endpoint(t, "email.delivered")
	w.apiMessage("m-1")
	w.event(t, "m-1", es.EventDelivery)
	if len(w.statuses.marked) != 1 || w.statuses.marked[0] != "m-1|delivered|delivery" {
		t.Fatalf("marked = %v", w.statuses.marked)
	}
	if fan := w.queue.take(TopicFanout); len(fan) != 1 || len(w.deliveries.rows) != 0 {
		t.Fatalf("fanout = %d deliveries %d", len(fan), len(w.deliveries.rows))
	}
}

func TestADeliveredEventReachesEverySubscribedEndpointSigned(t *testing.T) {
	w := newWorld(t)
	a := w.endpoint(t, "email.delivered")
	w.endpoint(t, "email.bounced")
	w.apiMessage("m-1")
	w.event(t, "m-1", es.EventDelivery)
	w.drain(t)
	if len(w.poster.calls) != 1 {
		t.Fatalf("posts = %d, want only the subscribed endpoint", len(w.poster.calls))
	}
	call := w.poster.calls[0]
	if call.URL != "https://hooks.acme.com/email" || call.Headers["User-Agent"] != "Acme Webhooks" || call.Headers["X-Webhook-Attempt"] != "1" || !strings.HasPrefix(call.Headers["X-Webhook-Id"], "evt_") {
		t.Fatalf("headers = %+v", call.Headers)
	}
	if err := wh.Verify(a.Secret, call.Headers["X-Webhook-Signature"], call.Body, w.now); err != nil {
		t.Fatalf("signature = %v", err)
	}
	var body struct {
		Type string `json:"type"`
		Data struct {
			MessageID string `json:"message_id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(call.Body, &body)
	if body.Type != "email.delivered" || body.Data.MessageID != "row-m-1" {
		t.Fatalf("body = %s", call.Body)
	}
	for _, d := range w.deliveries.rows {
		if d.Status != wh.StatusDelivered || len(w.deliveries.attempts[d.ID]) != 1 {
			t.Fatalf("delivery = %+v", d)
		}
	}
}

func TestTheSameEventFannedOutTwiceIsDeliveredOnce(t *testing.T) {
	w := newWorld(t)
	w.endpoint(t, "email.delivered")
	w.apiMessage("m-1")
	msg := FanoutMessage{MessageID: "m-1", Kind: es.EventDelivery, OccurredAt: w.now}
	for i := 0; i < 2; i++ {
		if err := w.module.Fanout(context.Background(), msg); err != nil {
			t.Fatal(err)
		}
	}
	w.drain(t)
	if len(w.poster.calls) != 1 {
		t.Fatalf("posts = %d", len(w.poster.calls))
	}
}

func TestAFailureSchedulesARetryThatCarriesItsAttempt(t *testing.T) {
	w := newWorld(t)
	w.endpoint(t, "email.delivered")
	w.apiMessage("m-1")
	w.poster.statuses = []int{500, 200}
	w.event(t, "m-1", es.EventDelivery)
	w.drain(t)
	var d *wh.Delivery
	for _, row := range w.deliveries.rows {
		d = row
	}
	if d.Status != wh.StatusRetrying || d.NextAttemptAt == nil || !d.NextAttemptAt.Equal(w.now.Add(30*time.Second)) || d.LastStatusCode != 500 {
		t.Fatalf("delivery = %+v", d)
	}
	if err := w.module.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.drain(t)
	if d.Status != wh.StatusRetrying || len(w.poster.calls) != 1 {
		t.Fatal("a retry must wait for its time")
	}
	w.now = w.now.Add(31 * time.Second)
	if err := w.module.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.drain(t)
	if d.Status != wh.StatusDelivered || len(w.poster.calls) != 2 || w.poster.calls[1].Headers["X-Webhook-Attempt"] != "2" {
		t.Fatalf("after the retry = %+v, posts %d", d, len(w.poster.calls))
	}
	attempts := w.deliveries.attempts[d.ID]
	if len(attempts) != 2 || attempts[0].StatusCode != 500 || attempts[0].NextAttemptAt == nil || attempts[1].StatusCode != 200 {
		t.Fatalf("attempts = %+v", attempts)
	}
	if w.poster.calls[0].Headers["X-Webhook-Id"] != w.poster.calls[1].Headers["X-Webhook-Id"] {
		t.Fatal("the event id must be stable across retries")
	}
}

func TestAStaleCopyOfADeliveryIsDropped(t *testing.T) {
	w := newWorld(t)
	w.endpoint(t, "email.delivered")
	w.apiMessage("m-1")
	w.event(t, "m-1", es.EventDelivery)
	w.drain(t)
	var id string
	for k := range w.deliveries.rows {
		id = k
	}
	if err := w.module.Deliver(context.Background(), DeliveryMessage{DeliveryID: id, EndpointID: w.deliveries.rows[id].EndpointID, Attempt: 1}); err != nil {
		t.Fatal(err)
	}
	if len(w.poster.calls) != 1 {
		t.Fatalf("a duplicate copy posted again: %d", len(w.poster.calls))
	}
}

func TestAFullEndpointWaitsWithoutSpendingAnAttempt(t *testing.T) {
	w := newWorld(t)
	e := w.endpoint(t, "email.delivered")
	w.apiMessage("m-1")
	w.slots.inUse[e.Endpoint.ID] = wh.MaxInFlightPerEndpoint
	w.event(t, "m-1", es.EventDelivery)
	w.drain(t)
	waiting := w.queue.take(TopicDelivery)
	if len(waiting) != 1 || waiting[0].Delay != wh.SlotWait || len(w.poster.calls) != 0 {
		t.Fatalf("waiting = %+v posts %d", waiting, len(w.poster.calls))
	}
	for _, d := range w.deliveries.rows {
		if d.Attempts != 0 || d.Status != wh.StatusQueued {
			t.Fatalf("delivery = %+v", d)
		}
	}
	w.slots.err = errors.New("redis down")
	var msg DeliveryMessage
	_ = json.Unmarshal(waiting[0].Payload, &msg)
	w.slots.inUse[e.Endpoint.ID] = 0
	if err := w.module.Deliver(context.Background(), msg); err != nil || len(w.poster.calls) != 0 {
		t.Fatalf("without the slot store nothing goes out: %v %d", err, len(w.poster.calls))
	}
}

func TestAPausedEndpointSkipsItsQueuedDeliveries(t *testing.T) {
	w := newWorld(t)
	e := w.endpoint(t, "email.delivered")
	w.apiMessage("m-1")
	w.event(t, "m-1", es.EventDelivery)
	_ = w.module.Fanout(context.Background(), FanoutMessage{MessageID: "m-1", Kind: es.EventDelivery, OccurredAt: w.now})
	off := false
	if _, err := w.module.Update(context.Background(), UpdateInput{WorkspaceID: "ws-1", ID: e.Endpoint.ID, Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	w.drain(t)
	for _, d := range w.deliveries.rows {
		if d.Status != wh.StatusSkipped {
			t.Fatalf("delivery = %+v", d)
		}
	}
	if len(w.poster.calls) != 0 {
		t.Fatal("a paused endpoint received a post")
	}
}

func TestTheLastFailedAttemptFailsTheDeliveryWhichCanBeRetriedByHand(t *testing.T) {
	w := newWorld(t)
	e := w.endpoint(t, "email.delivered")
	w.apiMessage("m-1")
	w.poster.err = errors.New("connection refused")
	w.event(t, "m-1", es.EventDelivery)
	w.drain(t)
	for i := 0; i < wh.MaxAttempts; i++ {
		w.now = w.now.Add(7 * time.Hour)
		_ = w.module.Sweep(context.Background())
		w.drain(t)
	}
	var d *wh.Delivery
	for _, row := range w.deliveries.rows {
		d = row
	}
	if d.Status != wh.StatusFailed || d.Attempts != wh.MaxAttempts || len(w.poster.calls) != wh.MaxAttempts {
		t.Fatalf("delivery = %+v posts %d", d, len(w.poster.calls))
	}
	w.poster.err = nil
	if _, err := w.module.Retry(context.Background(), "ws-1", e.Endpoint.ID, d.ID); err != nil {
		t.Fatal(err)
	}
	w.drain(t)
	if d.Status != wh.StatusDelivered {
		t.Fatalf("manual retry = %+v", d)
	}
}

func TestAnEndpointFailingForAnHourRaisesOneEpisodeAnHourAndRecoveryResolvesIt(t *testing.T) {
	w := newWorld(t)
	e := w.endpoint(t, "email.delivered")
	w.poster.err = errors.New("timeout")
	for i := 0; i < 5; i++ {
		id := "m-" + string(rune('a'+i))
		w.apiMessage(id)
		w.event(t, id, es.EventDelivery)
		w.drain(t)
		w.now = w.now.Add(20 * time.Minute)
	}
	if len(w.alerts.published) != 1 {
		t.Fatalf("alerts = %d, want one after the first hour", len(w.alerts.published))
	}
	d := w.alerts.published[0]
	if d.EpisodeKey != wh.AlertKey(e.Endpoint.ID) || !d.Email || d.Audience.Permission == nil || *d.Audience.Permission != (workspace.PermissionEntry{Resource: workspace.ResourceEmailMarketing, Action: workspace.ActionConfigure}) || d.Source != notification.SourceEmailWebhook {
		t.Fatalf("draft = %+v", d)
	}
	if strings.ContainsAny(d.Title+d.Body, "–—") {
		t.Fatal("dashes in the alert copy")
	}
	if !w.endpoints.endpoints[e.Endpoint.ID].Enabled {
		t.Fatal("a failing endpoint must never be disabled by the system")
	}
	w.poster.err = nil
	w.apiMessage("m-ok")
	w.event(t, "m-ok", es.EventDelivery)
	w.drain(t)
	if len(w.alerts.resolved) != 1 || w.alerts.resolved[0] != wh.AlertKey(e.Endpoint.ID) || w.endpoints.endpoints[e.Endpoint.ID].FailingSince != nil {
		t.Fatalf("resolved = %v", w.alerts.resolved)
	}
}

func TestDeliveriesPageCarriesTheAttempts(t *testing.T) {
	w := newWorld(t)
	e := w.endpoint(t, "email.delivered")
	w.apiMessage("m-1")
	w.event(t, "m-1", es.EventDelivery)
	w.drain(t)
	page, err := w.module.Deliveries(context.Background(), "ws-1", e.Endpoint.ID, "", 10)
	if err != nil || len(page.Items) != 1 || len(page.Items[0].Attempts) != 1 {
		t.Fatalf("page = %+v %v", page, err)
	}
	if _, err := w.module.Deliveries(context.Background(), "ws-2", e.Endpoint.ID, "", 10); !errors.Is(err, wh.ErrEndpointNotFound) {
		t.Fatalf("another workspace = %v", err)
	}
}

func TestAFailedFanoutRetriesWithItsAttemptAndThenGivesUp(t *testing.T) {
	w := newWorld(t)
	msg := FanoutMessage{MessageID: "m-x", Kind: es.EventDelivery, OccurredAt: w.now}
	w.module.d.Entries = failingEntries{}
	if err := w.module.Fanout(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	retry := w.queue.take(TopicFanout)
	var next FanoutMessage
	_ = json.Unmarshal(retry[0].Payload, &next)
	if len(retry) != 1 || next.Attempt != 1 || retry[0].Delay != 5*time.Second {
		t.Fatalf("retry = %+v", retry)
	}
	msg.Attempt = fanoutRetries
	if err := w.module.Fanout(context.Background(), msg); err != nil || len(w.queue.take(TopicFanout)) != 0 {
		t.Fatal("the last attempt gives up")
	}
}

type failingEntries struct{}

func (failingEntries) GetByMessageID(context.Context, string) (*emailapi.LogEntry, error) {
	return nil, errors.New("db down")
}
