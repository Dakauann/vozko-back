package emailwiring_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"gorm.io/gorm"

	"vozko/delivery/http/apikeys"
	"vozko/delivery/http/emailapihttp"
	"vozko/domain/emailaddress"
	es "vozko/domain/emailsending"
	wh "vozko/domain/emailwebhook"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/database/schema"
	"vozko/infra/emailwiring"
	apikey_repository "vozko/infra/repositories/apikey"
	"vozko/infra/ses/sesfake"
	apikey_usecase "vozko/usecases/apikey"
	emailwebhook_usecase "vozko/usecases/emailwebhook"
)

type openLimiter struct{}

func (openLimiter) Allow(string) (bool, time.Duration, error) { return true, 0, nil }

type onlyHost string

func (h onlyHost) CheckHost(_ context.Context, host string) error {
	if host != string(h) {
		return wh.ErrURLBlocked
	}
	return nil
}

type hookCall struct {
	Headers http.Header
	Body    []byte
}

type hookServer struct {
	*httptest.Server
	mu       sync.Mutex
	calls    []hookCall
	statuses []int
	served   atomic.Int64
}

func newHook(t *testing.T) *hookServer {
	t.Helper()
	h := &hookServer{}
	h.Server = httptest.NewTLSServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		h.mu.Lock()
		h.calls = append(h.calls, hookCall{Headers: r.Header.Clone(), Body: body})
		status := http.StatusOK
		if len(h.statuses) > 0 {
			status, h.statuses = h.statuses[0], h.statuses[1:]
		}
		h.mu.Unlock()
		h.served.Add(1)
		rw.WriteHeader(status)
	}))
	t.Cleanup(h.Close)
	return h
}

func (h *hookServer) received() []hookCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]hookCall(nil), h.calls...)
}

type apiRig struct {
	w      *world
	api    *emailwiring.EmailAPI
	keys   *apikey_usecase.Service
	router *mux.Router
	hook   *hookServer
	mu     sync.Mutex
	now    time.Time
}

func (r *apiRig) clock() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.now
}

func (r *apiRig) advance(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.now = r.now.Add(d)
}

func (w *world) withEmailAPI() *apiRig {
	w.t.Helper()
	w.must(w.db.AutoMigrate(schema.EmailAPIModels()...))
	slots, ok := w.shared.(emailwiring.SlotState)
	if !ok {
		w.t.Fatal("the e2e shared state cannot release owned keys")
	}
	rig := &apiRig{w: w, hook: newHook(w.t), now: time.Now().UTC()}
	hookURL, err := url.Parse(rig.hook.URL)
	w.must(err)
	keys, err := apikey_usecase.New(apikey_usecase.Deps{Keys: apikey_repository.New(w.db), Cache: w.shared})
	w.must(err)
	rig.keys = keys
	rig.api, err = emailwiring.NewEmailAPI(w.db, emailwiring.EmailAPIAdapters{
		Sending: w.module, Billing: w.billing, Files: w.files, QueuePub: w.queue, Shared: slots, Alerts: w.alerts,
		Cipher: piigorm.Service(), Hosts: onlyHost(hookURL.Hostname()), Client: rig.hook.Client(), UserAgent: "E2E Webhooks", Now: rig.clock,
	})
	w.must(err)
	for _, c := range rig.api.Consumers(w.queue, w.queue, w.shared) {
		w.must(c.Start())
	}
	rig.router = mux.NewRouter()
	emailapihttp.RegisterKeyRoutes(rig.router, emailapihttp.NewHandler(rig.api.Service, rig.api.Service, rig.api.Webhooks), apikeys.NewAuthenticator(keys, openLimiter{}, openLimiter{}))
	return rig
}

func (r *apiRig) key(ws *tenant, scopes ...string) apikey_usecase.Created {
	r.w.t.Helper()
	created, err := r.keys.Create(r.w.ctx, apikey_usecase.CreateInput{WorkspaceID: ws.ID, UserID: ws.Actor.UserID, Name: "Loja", Scopes: scopes})
	r.w.must(err)
	return created
}

func (r *apiRig) endpoint(ws *tenant, events ...string) emailwebhook_usecase.WithSecret {
	r.w.t.Helper()
	created, err := r.api.Webhooks.Create(r.w.ctx, emailwebhook_usecase.CreateInput{WorkspaceID: ws.ID, URL: r.hook.URL + "/email-events", Events: events, Enabled: true})
	r.w.must(err)
	return created
}

func (r *apiRig) call(method, path, token string, body any) *httptest.ResponseRecorder {
	r.w.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		r.w.must(json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.router.ServeHTTP(rec, req)
	return rec
}

func (r *apiRig) send(token string, body map[string]any) (emailapihttp.SendResponse, *httptest.ResponseRecorder) {
	r.w.t.Helper()
	rec := r.call(http.MethodPost, emailapihttp.KeyRoutesPrefix, token, body)
	var out emailapihttp.SendResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out, rec
}

func (r *apiRig) pump() {
	r.w.t.Helper()
	for i := 0; i < 200; i++ {
		moved := r.w.queue.drain(r.w.t, emailwebhook_usecase.TopicFanout, 4, 0)
		moved += r.w.queue.drain(r.w.t, emailwebhook_usecase.TopicDelivery, 20, 0)
		if r.w.queue.delayedCount(emailwebhook_usecase.TopicDelivery) > 0 {
			moved += r.w.queue.release(emailwebhook_usecase.TopicDelivery)
		}
		if moved == 0 {
			return
		}
	}
	r.w.t.Fatal("the webhook queues never emptied")
}

func (r *apiRig) delivery(endpointID string) schema.EmailWebhookDelivery {
	r.w.t.Helper()
	var rows []schema.EmailWebhookDelivery
	r.w.must(r.w.db.Where("endpoint_id = ?", endpointID).Find(&rows).Error)
	if len(rows) != 1 {
		r.w.t.Fatalf("deliveries of %s = %d, want one", endpointID, len(rows))
	}
	return rows[0]
}

func resetEmail(to, key string) map[string]any {
	return map[string]any{
		"from": "", "to": []string{to}, "subject": "Seu código", "html": "<p>Código <b>482913</b></p>", "text": "Código 482913",
		"reply_to": []string{"suporte@loja.com.br"}, "tags": []map[string]string{{"name": "kind", "value": "reset"}}, "idempotency_key": key,
	}
}

func (w *world) senderAddress(ws *tenant) string {
	return string(ws.Sender.FromAddress(ws.Domain))
}

func TestEmailE2E_AnAPIKeySendIsDeliveredAndReportedToASignedWebhook(t *testing.T) {
	w := newWorld(t)
	rig := w.withEmailAPI()
	ws := w.sendingTenant("Loja da Ana", 5_000_000)
	key := rig.key(ws, "email:send", "email:read")
	hook := rig.endpoint(ws, "email.sent", "email.delivered")
	body := resetEmail("ana@cliente.com", "reset-1")
	body["from"] = w.senderAddress(ws)

	res, rec := rig.send(key.Plain, body)
	if rec.Code != http.StatusCreated || len(res.Messages) != 1 || res.Messages[0].Status != "sent" {
		t.Fatalf("send = %d %s", rec.Code, rec.Body.String())
	}
	sent := w.onlySentTo("ana@cliente.com")
	if len(sent.Message.ReplyTo) != 1 || sent.Message.ReplyTo[0] != "suporte@loja.com.br" || sent.Message.Tags["run_kind"] != string(es.RunAPI) {
		t.Fatalf("outbound = %+v", sent.Message)
	}
	if w.conversationsOf(ws) != 0 {
		t.Fatal("an API send must not reach the inbox")
	}
	var leads int64
	w.must(w.db.Raw("SELECT count(*) FROM leads WHERE workspace_id = ?", ws.ID).Scan(&leads).Error)
	if leads != 0 {
		t.Fatal("an API send must not create leads")
	}
	if got := ws.Start - w.wallet(ws); got != w.unit(ws) {
		t.Fatalf("charged %d, want one email", got)
	}

	w.providerEvent(sesfake.Delivery, sent)
	rig.pump()

	calls := rig.hook.received()
	if len(calls) != 1 {
		t.Fatalf("webhook calls = %d", len(calls))
	}
	if err := wh.Verify(hook.Secret, calls[0].Headers.Get("X-Webhook-Signature"), calls[0].Body, time.Now()); err != nil {
		t.Fatalf("signature = %v", err)
	}
	var event struct {
		Type string `json:"type"`
		Data struct {
			MessageID      string            `json:"message_id"`
			IdempotencyKey string            `json:"idempotency_key"`
			To             string            `json:"to"`
			Tags           map[string]string `json:"tags"`
		} `json:"data"`
	}
	w.must(json.Unmarshal(calls[0].Body, &event))
	if event.Type != "email.delivered" || event.Data.MessageID != res.Messages[0].ID || event.Data.IdempotencyKey != "reset-1" || event.Data.To != "ana@cliente.com" || event.Data.Tags["kind"] != "reset" {
		t.Fatalf("event = %s", calls[0].Body)
	}
	if d := rig.delivery(hook.Endpoint.ID); d.Status != string(wh.StatusDelivered) || d.Attempts != 1 {
		t.Fatalf("delivery = %+v", d)
	}

	logRec := rig.call(http.MethodGet, emailapihttp.KeyRoutesPrefix+"/"+res.Messages[0].ID, key.Plain, nil)
	var detail emailapihttp.LogDetailResponse
	w.must(json.Unmarshal(logRec.Body.Bytes(), &detail))
	if logRec.Code != http.StatusOK || detail.Status != "delivered" || len(detail.Events) == 0 {
		t.Fatalf("detail = %d %s", logRec.Code, logRec.Body.String())
	}
	listRec := rig.call(http.MethodGet, emailapihttp.KeyRoutesPrefix+"?tag_name=kind&tag_value=reset&status=delivered", key.Plain, nil)
	var page emailapihttp.LogPageResponse
	w.must(json.Unmarshal(listRec.Body.Bytes(), &page))
	if len(page.Data) != 1 || page.Data[0].ID != res.Messages[0].ID {
		t.Fatalf("log = %s", listRec.Body.String())
	}
}

func TestEmailE2E_AWebhookAnsweringFiveHundredIsRetriedWithItsAttempt(t *testing.T) {
	w := newWorld(t)
	rig := w.withEmailAPI()
	ws := w.sendingTenant("Loja do Bruno", 5_000_000)
	key := rig.key(ws, "email:send")
	hook := rig.endpoint(ws, "email.delivered")
	rig.hook.statuses = []int{http.StatusInternalServerError}
	body := resetEmail("bruno@cliente.com", "reset-2")
	body["from"] = w.senderAddress(ws)
	if _, rec := rig.send(key.Plain, body); rec.Code != http.StatusCreated {
		t.Fatalf("send = %d %s", rec.Code, rec.Body.String())
	}
	w.providerEvent(sesfake.Delivery, w.onlySentTo("bruno@cliente.com"))
	rig.pump()
	first := rig.delivery(hook.Endpoint.ID)
	if first.Status != string(wh.StatusRetrying) || first.LastStatusCode != 500 || first.NextAttemptAt == nil {
		t.Fatalf("after the 500 = %+v", first)
	}
	w.must(rig.api.Webhooks.Sweep(w.ctx))
	rig.pump()
	if len(rig.hook.received()) != 1 {
		t.Fatal("the retry must wait for its backoff")
	}
	rig.advance(31 * time.Second)
	w.must(rig.api.Webhooks.Sweep(w.ctx))
	rig.pump()
	calls := rig.hook.received()
	if len(calls) != 2 || calls[1].Headers.Get("X-Webhook-Attempt") != "2" || calls[0].Headers.Get("X-Webhook-Id") != calls[1].Headers.Get("X-Webhook-Id") {
		t.Fatalf("calls = %d, attempt %q", len(calls), calls[len(calls)-1].Headers.Get("X-Webhook-Attempt"))
	}
	if d := rig.delivery(hook.Endpoint.ID); d.Status != string(wh.StatusDelivered) || d.Attempts != 2 {
		t.Fatalf("delivery = %+v", d)
	}
	var attempts []schema.EmailWebhookAttempt
	w.must(w.db.Where("delivery_id = ?", first.ID).Order("attempt").Find(&attempts).Error)
	if len(attempts) != 2 || attempts[0].StatusCode != 500 || attempts[1].StatusCode != 200 {
		t.Fatalf("attempts = %+v", attempts)
	}
}

func TestEmailE2E_AReplayedKeyNeverSendsOrChargesTwice(t *testing.T) {
	w := newWorld(t)
	rig := w.withEmailAPI()
	ws := w.sendingTenant("Loja da Carla", 5_000_000)
	key := rig.key(ws, "email:send")
	body := resetEmail("carla@cliente.com", "reset-3")
	body["from"] = w.senderAddress(ws)
	body["bcc"] = []string{"auditoria@loja.com.br"}
	first, rec := rig.send(key.Plain, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("first = %d %s", rec.Code, rec.Body.String())
	}
	var wg sync.WaitGroup
	statuses := make(chan int, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, again := rig.send(key.Plain, body)
			statuses <- again.Code
		}()
	}
	wg.Wait()
	close(statuses)
	for s := range statuses {
		if s != http.StatusOK && s != http.StatusConflict {
			t.Fatalf("replay status = %d", s)
		}
	}
	again, rec := rig.send(key.Plain, body)
	if rec.Code != http.StatusOK || !again.Repeated || again.ID != first.ID || len(w.ses.Sent()) != 2 {
		t.Fatalf("replay = %d %+v, provider sends %d", rec.Code, again, len(w.ses.Sent()))
	}
	if debits := w.ledger(ws, "debit", "email_api:%"); len(debits) != 1 || sumOf(debits) != 2*w.unit(ws) {
		t.Fatalf("debits = %+v", debits)
	}
	other := resetEmail("carla@cliente.com", "reset-3")
	other["from"] = w.senderAddress(ws)
	other["subject"] = "Outro assunto"
	if _, rec := rig.send(key.Plain, other); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "email_idempotency_key_reused") {
		t.Fatalf("reused key = %d %s", rec.Code, rec.Body.String())
	}
}

func TestEmailE2E_AShortWalletRefusesTheAPISendAndChargesNothing(t *testing.T) {
	w := newWorld(t)
	rig := w.withEmailAPI()
	ws := w.sendingTenant("Loja do Davi", 0)
	key := rig.key(ws, "email:send")
	body := resetEmail("davi@cliente.com", "reset-4")
	body["from"] = w.senderAddress(ws)
	_, rec := rig.send(key.Plain, body)
	if rec.Code != http.StatusPaymentRequired || !strings.Contains(rec.Body.String(), "email_balance_insufficient") || !strings.Contains(rec.Body.String(), "missing_micros") {
		t.Fatalf("short wallet = %d %s", rec.Code, rec.Body.String())
	}
	if len(w.ses.Sent()) != 0 || len(w.ledger(ws, "debit", "email_api:%")) != 0 {
		t.Fatal("a refused send reached the provider or the ledger")
	}
	w.topUp(ws, 1_000_000)
	if _, rec := rig.send(key.Plain, body); rec.Code != http.StatusOK || len(w.ses.Sent()) != 1 {
		t.Fatalf("after the top up = %d %s", rec.Code, rec.Body.String())
	}
}

func TestEmailE2E_ARevokedKeyIsRefusedAtOnce(t *testing.T) {
	w := newWorld(t)
	rig := w.withEmailAPI()
	ws := w.sendingTenant("Loja da Eva", 5_000_000)
	key := rig.key(ws, "email:send", "email:read")
	if rec := rig.call(http.MethodGet, emailapihttp.KeyRoutesPrefix, key.Plain, nil); rec.Code != http.StatusOK {
		t.Fatalf("before revoke = %d", rec.Code)
	}
	_, err := rig.keys.Revoke(w.ctx, ws.ID, key.Key.ID, ws.Actor.UserID)
	w.must(err)
	body := resetEmail("eva@cliente.com", "reset-5")
	body["from"] = w.senderAddress(ws)
	if _, rec := rig.send(key.Plain, body); rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "api_key_revoked") {
		t.Fatalf("revoked = %d %s", rec.Code, rec.Body.String())
	}
	if len(w.ses.Sent()) != 0 {
		t.Fatal("a revoked key sent an email")
	}
	readOnly := rig.key(ws, "email:read")
	if _, rec := rig.send(readOnly.Plain, body); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "api_key_scope_missing") {
		t.Fatalf("read only key = %d %s", rec.Code, rec.Body.String())
	}
}

type queryCounter struct {
	mu     sync.Mutex
	counts map[string]int64
	on     atomic.Bool
}

func countQueries(t *testing.T, db *gorm.DB) *queryCounter {
	t.Helper()
	c := &queryCounter{counts: map[string]int64{}}
	hook := func(kind string) func(*gorm.DB) {
		return func(*gorm.DB) {
			if c.on.Load() {
				c.mu.Lock()
				c.counts[kind]++
				c.mu.Unlock()
			}
		}
	}
	cb := db.Callback()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	name := "e2e:count:" + uuid.NewString()
	must(cb.Create().After("gorm:create").Register(name, hook("create")))
	must(cb.Query().After("gorm:query").Register(name, hook("query")))
	must(cb.Update().After("gorm:update").Register(name, hook("update")))
	must(cb.Delete().After("gorm:delete").Register(name, hook("delete")))
	must(cb.Row().After("gorm:row").Register(name, hook("row")))
	must(cb.Raw().After("gorm:raw").Register(name, hook("raw")))
	return c
}

func (c *queryCounter) take() map[string]int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.counts
	c.counts = map[string]int64{}
	return out
}

func total(counts map[string]int64) int64 {
	var n int64
	for _, v := range counts {
		n += v
	}
	return n
}

func TestEmailE2E_ThousandsOfEventsFanOutWithBoundedQueries(t *testing.T) {
	const events = 2000
	w := newWorld(t)
	rig := w.withEmailAPI()
	ws := w.sendingTenant("Loja em escala", 5_000_000)
	endpoints := []emailwebhook_usecase.WithSecret{rig.endpoint(ws, "email.delivered"), rig.endpoint(ws, "email.delivered", "email.bounced")}
	rig.endpoint(ws, "email.opened")
	sendID := uuid.NewString()
	w.must(w.db.Exec(`INSERT INTO email_api_sends (id, workspace_id, idempotency_key, fingerprint, origin, sender_id, from_address, subject, tags, recipients, round_settled, created_at, updated_at)
		VALUES (?, ?, 'bulk', repeat('a', 64), 'api', ?, 'no-reply@loja.com.br', 'Oi', '{}', ?, true, now(), now())`, sendID, ws.ID, ws.Sender.ID, events).Error)
	w.must(w.db.Exec(`INSERT INTO email_api_messages (id, send_id, position, workspace_id, role, address, status, error_code, message_id, round, last_event, created_at, updated_at)
		SELECT gen_random_uuid(), ?, g, ?, 'to', 'p' || g || '@cliente.com', 'sent', '', gen_random_uuid(), 1, '', now(), now() FROM generate_series(0, ?) g`, sendID, ws.ID, events-1).Error)
	var messageIDs []string
	w.must(w.db.Raw("SELECT message_id FROM email_api_messages WHERE send_id = ? ORDER BY position", sendID).Scan(&messageIDs).Error)

	counter := countQueries(t, w.db)
	counter.on.Store(true)
	started := time.Now()
	for _, id := range messageIDs {
		w.must(rig.api.Webhooks.OnMessageEvent(w.ctx, es.Message{ID: id, WorkspaceID: ws.ID, RunKind: es.RunAPI, Status: es.MessageDelivered, Address: emailaddress.Address("x@y.com")},
			es.DeliveryEvent{Kind: es.EventDelivery, OccurredAt: time.Now().UTC()}, es.EventEffect{Changed: true}))
	}
	observed := counter.take()
	w.queue.drain(t, emailwebhook_usecase.TopicFanout, 8, 0)
	fanned := counter.take()
	rig.pump()
	delivered := counter.take()
	counter.on.Store(false)

	if got := rig.hook.served.Load(); got != int64(events*len(endpoints)) {
		t.Fatalf("webhook calls = %d, want %d", got, events*len(endpoints))
	}
	var pending int64
	w.must(w.db.Model(&schema.EmailWebhookDelivery{}).Where("status <> ?", string(wh.StatusDelivered)).Count(&pending).Error)
	if pending != 0 {
		t.Fatalf("%d deliveries did not finish", pending)
	}
	perEvent := func(c map[string]int64) float64 { return float64(total(c)) / events }
	perDelivery := float64(total(delivered)) / float64(events*len(endpoints))
	t.Logf("%d events to %d endpoints in %s", events, len(endpoints), time.Since(started))
	t.Logf("observer: %v (%.2f per event)", observed, perEvent(observed))
	t.Logf("fan out: %v (%.2f per event)", fanned, perEvent(fanned))
	t.Logf("deliveries: %v (%.2f per delivery)", delivered, perDelivery)
	if perEvent(observed) > 1 {
		t.Fatalf("the observer made %.2f queries per event, want one indexed write", perEvent(observed))
	}
	if perEvent(fanned) > 3 {
		t.Fatalf("the fan out made %.2f queries per event, want at most three", perEvent(fanned))
	}
	if perDelivery > 4 {
		t.Fatalf("each delivery made %.2f queries, want at most four", perDelivery)
	}
}
