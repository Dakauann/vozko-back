package emailwebhook_repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/emailapi"
	wh "vozko/domain/emailwebhook"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestInsertAndRequeueSQLBindEveryPlaceholder(t *testing.T) {
	if got := strings.Count(InsertSQL(3), "?"); got != 30 {
		t.Fatalf("insert placeholders = %d", got)
	}
	if got := strings.Count(requeueSQL, "?"); got != 9 {
		t.Fatalf("requeue placeholders = %d", got)
	}
}

func webhookDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := repotest.IsolatedDB(t, "email_webhook_test", schema.EmailAPIModels()...)
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(40)
	return db
}

func endpoint(ws string, events ...wh.EventType) wh.Endpoint {
	now := time.Now().UTC()
	return wh.Endpoint{ID: uuid.NewString(), WorkspaceID: ws, URL: "https://hooks.example.com/x", Events: events, Enabled: true,
		SecretCipher: []byte("cipher"), SecretHint: "abcd", CreatedAt: now, UpdatedAt: now}
}

func TestIntegrationEndpointsStayWithinTheLimitAndFilterBySubscription(t *testing.T) {
	db := webhookDB(t)
	store := NewEndpoints(db)
	ctx := context.Background()
	ws := uuid.NewString()
	var created atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < wh.MaxEndpointsPerWorkspace+5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			e := endpoint(ws, wh.EventDelivered)
			if i%2 == 0 {
				e.Events = []wh.EventType{wh.EventBounced, wh.EventDelivered}
			}
			switch err := store.Create(ctx, &e, wh.MaxEndpointsPerWorkspace); {
			case err == nil:
				created.Add(1)
			case !errors.Is(err, wh.ErrLimitReached):
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if created.Load() != wh.MaxEndpointsPerWorkspace {
		t.Fatalf("created = %d", created.Load())
	}
	all, _ := store.List(ctx, ws)
	all[0].Enabled = false
	all[0].UpdatedAt = time.Now()
	if err := store.Update(ctx, &all[0]); err != nil {
		t.Fatal(err)
	}
	delivered, _ := store.Subscribed(ctx, ws, wh.EventDelivered)
	bounced, _ := store.Subscribed(ctx, ws, wh.EventBounced)
	opened, _ := store.Subscribed(ctx, ws, wh.EventOpened)
	if len(delivered) != wh.MaxEndpointsPerWorkspace-1 || len(bounced) == 0 || len(opened) != 0 {
		t.Fatalf("subscribed delivered %d bounced %d opened %d", len(delivered), len(bounced), len(opened))
	}
	if err := store.MarkFailure(ctx, all[1].ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	first, _ := store.Get(ctx, ws, all[1].ID)
	if err := store.MarkFailure(ctx, all[1].ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	second, _ := store.Get(ctx, ws, all[1].ID)
	if first.FailingSince == nil || !second.FailingSince.Equal(*first.FailingSince) {
		t.Fatal("failing_since keeps the first failure")
	}
	_ = store.MarkSuccess(ctx, all[1].ID, time.Now())
	if healed, _ := store.Get(ctx, ws, all[1].ID); healed.FailingSince != nil || healed.LastSuccessAt == nil {
		t.Fatalf("healed = %+v", healed)
	}
	if err := store.Delete(ctx, uuid.NewString(), all[1].ID); !errors.Is(err, wh.ErrEndpointNotFound) {
		t.Fatalf("delete from another workspace = %v", err)
	}
}

func seedDeliveries(t *testing.T, db *gorm.DB, n int) (*Deliveries, wh.Endpoint, []wh.Delivery) {
	t.Helper()
	ws := uuid.NewString()
	e := endpoint(ws, wh.EventDelivered)
	if err := NewEndpoints(db).Create(context.Background(), &e, 10); err != nil {
		t.Fatal(err)
	}
	store := NewDeliveries(db)
	batch := make([]wh.Delivery, n)
	now := time.Now().UTC()
	for i := range batch {
		batch[i] = wh.Delivery{ID: uuid.NewString(), EndpointID: e.ID, WorkspaceID: ws, EventID: fmt.Sprintf("evt_%d", i), EventType: wh.EventDelivered,
			MessageID: uuid.NewString(), Payload: []byte(`{"id":"x"}`), CreatedAt: now.Add(time.Duration(i) * time.Millisecond)}
	}
	inserted, err := store.InsertBatch(context.Background(), batch)
	if err != nil || len(inserted) != n {
		t.Fatalf("inserted %d, %v", len(inserted), err)
	}
	return store, e, inserted
}

func TestIntegrationDeliveriesDedupClaimFinishAndRetry(t *testing.T) {
	db := webhookDB(t)
	store, e, inserted := seedDeliveries(t, db, 3)
	ctx := context.Background()
	again := append([]wh.Delivery(nil), inserted...)
	for i := range again {
		again[i].ID = uuid.NewString()
	}
	if dup, err := store.InsertBatch(ctx, again); err != nil || len(dup) != 0 {
		t.Fatalf("the same events inserted again: %d %v", len(dup), err)
	}
	id := inserted[0].ID
	var wins atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, err := store.Claim(ctx, id, 1, time.Now()); ok && err == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("claims won = %d", wins.Load())
	}
	if ok, _ := store.Claim(ctx, id, 2, time.Now()); ok {
		t.Fatal("a stale attempt claimed a delivery being sent")
	}
	now := time.Now().UTC()
	next := now.Add(-time.Second)
	if err := store.Finish(ctx, wh.Finish{ID: id, Outcome: wh.Outcome{Status: wh.StatusRetrying, NextAttemptAt: &next}, StatusCode: 500, Error: "boom", At: now,
		Attempt: wh.Attempt{Attempt: 1, StatusCode: 500, DurationMS: 12, Error: "boom", NextAttemptAt: &next, AttemptedAt: now}}); err != nil {
		t.Fatal(err)
	}
	target, err := store.Target(ctx, id)
	if err != nil || target.Delivery.Status != wh.StatusRetrying || target.Delivery.Attempts != 1 || target.Endpoint.URL != e.URL || string(target.Endpoint.SecretCipher) != "cipher" {
		t.Fatalf("target = %+v %v", target, err)
	}
	due, err := store.Requeue(ctx, now, 200)
	if err != nil || len(due) != 1 || due[0].ID != id || due[0].Attempts != 1 {
		t.Fatalf("requeued = %+v %v", due, err)
	}
	if ok, _ := store.Claim(ctx, id, 2, now); !ok {
		t.Fatal("the requeued delivery takes attempt 2")
	}
	if err := store.Finish(ctx, wh.Finish{ID: id, Outcome: wh.Outcome{Status: wh.StatusFailed}, StatusCode: 404, At: now,
		Attempt: wh.Attempt{Attempt: 2, StatusCode: 404, AttemptedAt: now}}); err != nil {
		t.Fatal(err)
	}
	attempts, _ := store.Attempts(ctx, []string{id})
	if len(attempts[id]) != 2 || attempts[id][1].StatusCode != 404 {
		t.Fatalf("attempts = %+v", attempts)
	}
	if _, err := store.Retry(ctx, e.WorkspaceID, e.ID, inserted[1].ID, now); !errors.Is(err, wh.ErrDeliveryNotRetryable) {
		t.Fatalf("retry of a queued delivery = %v", err)
	}
	retried, err := store.Retry(ctx, e.WorkspaceID, e.ID, id, now)
	if err != nil || retried.Status != wh.StatusQueued || retried.Attempts != 2 {
		t.Fatalf("retried = %+v %v", retried, err)
	}
	if _, err := store.Retry(ctx, uuid.NewString(), e.ID, id, now); !errors.Is(err, wh.ErrDeliveryNotFound) {
		t.Fatalf("retry from another workspace = %v", err)
	}
	page, err := store.ListByEndpoint(ctx, e.WorkspaceID, e.ID, emailapi.Cursor{}, 2)
	if err != nil || len(page) != 3 {
		t.Fatalf("page = %d %v", len(page), err)
	}
}

func TestIntegrationRequeueTakesStaleWorkOnly(t *testing.T) {
	db := webhookDB(t)
	store, _, inserted := seedDeliveries(t, db, 3)
	ctx := context.Background()
	now := time.Now().UTC()
	if due, _ := store.Requeue(ctx, now, 200); len(due) != 0 {
		t.Fatalf("fresh work was requeued: %+v", due)
	}
	_, _ = store.Claim(ctx, inserted[0].ID, 1, now.Add(-3*time.Minute))
	db.Model(&schema.EmailWebhookDelivery{}).Where("id = ?", inserted[1].ID).Update("updated_at", now.Add(-6*time.Minute))
	db.Model(&schema.EmailWebhookDelivery{}).Where("id = ?", inserted[2].ID).Update("updated_at", now.Add(-3*time.Minute))
	due, err := store.Requeue(ctx, now, 200)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, d := range due {
		got[d.ID] = d.Attempts
	}
	if len(got) != 2 || got[inserted[0].ID] != 1 || got[inserted[1].ID] != 0 {
		t.Fatalf("requeued = %+v, want the stale sending and the stale queued delivery", due)
	}
}

func TestIntegrationPurgeDeletesInBoundedChunks(t *testing.T) {
	db := webhookDB(t)
	store, _, inserted := seedDeliveries(t, db, 25)
	ctx := context.Background()
	old := time.Now().Add(-40 * 24 * time.Hour)
	for i, d := range inserted {
		db.Model(&schema.EmailWebhookDelivery{}).Where("id = ?", d.ID).Updates(map[string]any{"status": "delivered", "created_at": old})
		db.Create(&schema.EmailWebhookAttempt{DeliveryID: d.ID, Attempt: 1, AttemptedAt: old.Add(time.Duration(i) * time.Second)})
	}
	cutoff := time.Now().Add(-wh.Retention)
	n, err := store.PurgeAttempts(ctx, cutoff, 10)
	if err != nil || n != 10 {
		t.Fatalf("first chunk = %d %v", n, err)
	}
	total := n
	for n == 10 {
		n, _ = store.PurgeAttempts(ctx, cutoff, 10)
		total += n
	}
	if total != 25 {
		t.Fatalf("purged attempts = %d", total)
	}
	if n, _ := store.PurgeDeliveries(ctx, cutoff, 100); n != 25 {
		t.Fatalf("purged deliveries = %d", n)
	}
}
