package mercadolivre

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/webhook"
	webhook_usecase "vozko/usecases/webhook"
)

func TestFailuresAreClassifiedFailClosed(t *testing.T) {
	classify := ClassifyNotificationFailure(fakeClassifier{}, newFakeMetrics())
	cases := []struct {
		name string
		err  error
		want webhook_usecase.Disposition
	}{
		{"unknown seller", ErrUnknownAccount, webhook_usecase.DispositionDrop},
		{"account marked after an auth failure", fmt.Errorf("%w: x", ErrAccountUnavailable), webhook_usecase.DispositionDrop},
		{"resource gone", fmt.Errorf("%w: x", ErrResourceGone), webhook_usecase.DispositionDrop},
		{"kind not handled yet", fmt.Errorf("%w: fetch_question", ErrJobKindNotHandled), webhook_usecase.DispositionDeadLetter},
		{"invalid resource", fmt.Errorf("%w: x", mldomain.ErrInvalidResource), webhook_usecase.DispositionDeadLetter},
		{"buyer unknown", mldomain.ErrBuyerUnknown, webhook_usecase.DispositionDeadLetter},
		{"undecodable answer", &mlErr{msg: "decode", decode: true}, webhook_usecase.DispositionDeadLetter},
		{"app blocked", &mlErr{msg: "unauthorized_scopes", appBlock: true}, webhook_usecase.DispositionDeadLetter},
		{"rate limited", &mlErr{msg: "429", rate: true}, webhook_usecase.DispositionRetry},
		{"transport", &mlErr{msg: "timeout", retryable: true}, webhook_usecase.DispositionRetry},
		{"token refreshed", fmt.Errorf("%w: x", errRetryWithFreshToken), webhook_usecase.DispositionRetry},
		{"refresh in progress", ErrTokenRefreshInProgress, webhook_usecase.DispositionRetry},
		{"database lookup", errors.New("connection reset"), webhook_usecase.DispositionRetry},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classify(tc.err); got != tc.want {
				t.Fatalf("disposition = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEveryQueueTopicHasAConsumer(t *testing.T) {
	topics := consumerTopics()
	if len(topics) != len(mldomain.QueueTopics()) {
		t.Fatalf("consumers = %v", topics)
	}
	for _, want := range mldomain.QueueTopics() {
		found := false
		for _, c := range topics {
			if c.topic == want && c.concurrency > 0 && strings.HasPrefix(c.name, "mercadolivre-") {
				found = true
			}
		}
		if !found {
			t.Errorf("no consumer for %s", want)
		}
	}
}

const testPathToken = "q3Jv9xZ0b2xH5nLw1aPq8sT4uV7yB6cD-eF_gH2iJkM"

func TestTheNotificationTokenIsComparedBeforeAnything(t *testing.T) {
	queue := newFakePublisher()
	metrics := newFakeMetrics()
	accept := NewAcceptNotification("5503910054141466", testPathToken, NewJobPublisher(queue), metrics)
	for _, token := range []string{"", "wrong", testPathToken + "x", strings.ToUpper(testPathToken)} {
		if err := accept.AcceptWithToken(token, notificationBody("questions", "/questions/1")); !errors.Is(err, ErrNotificationToken) {
			t.Fatalf("%q: err = %v", token, err)
		}
	}
	if queue.total() != 0 || metrics.count("notify/refused_token") != 4 {
		t.Fatalf("published=%d metrics=%v", queue.total(), metrics.events)
	}
	if err := accept.AcceptWithToken(testPathToken, notificationBody("questions", "/questions/1")); err != nil {
		t.Fatal(err)
	}
}

func notificationBody(topic, resource string) []byte {
	return []byte(fmt.Sprintf(`{"_id":"n-1","resource":%q,"user_id":415458330,"topic":%q,"application_id":5503910054141466,"attempts":1,"sent":"2026-10-19T12:00:00.000Z","received":"2026-10-19T12:00:00.000Z"}`, resource, topic))
}

func TestAcceptedNotificationsLandOnTheirFamilyTopic(t *testing.T) {
	queue := newFakePublisher()
	metrics := newFakeMetrics()
	accept := NewAcceptNotification("5503910054141466", testPathToken, NewJobPublisher(queue), metrics)
	cases := map[string]string{
		"messages":  webhook.TopicMercadoLivreMessage,
		"questions": webhook.TopicMercadoLivreQuestion,
		"orders_v2": webhook.TopicMercadoLivreOrder,
		"shipments": webhook.TopicMercadoLivreOrder,
		"items":     webhook.TopicMercadoLivreQuestion,
	}
	resources := map[string]string{
		"messages": "0123456789abcdef0123", "questions": "/questions/123", "orders_v2": "/orders/2000000000000001",
		"shipments": "/shipments/40000000001", "items": "/items/MLB123",
	}
	for topic, want := range cases {
		if err := accept.Accept(notificationBody(topic, resources[topic])); err != nil {
			t.Fatalf("%s: %v", topic, err)
		}
		payloads := queue.published[want]
		var job mldomain.Job
		if err := json.Unmarshal(payloads[len(payloads)-1], &job); err != nil || job.SellerID != "415458330" || job.IdempotencyKey() == "" {
			t.Fatalf("%s: job = %+v %v", topic, job, err)
		}
	}
	if metrics.count("notify/accepted") != len(cases) {
		t.Fatalf("metrics = %v", metrics.events)
	}
}

func TestRefusedNotificationsNeverReachTheQueue(t *testing.T) {
	queue := newFakePublisher()
	metrics := newFakeMetrics()
	accept := NewAcceptNotification("5503910054141466", testPathToken, NewJobPublisher(queue), metrics)
	for name, body := range map[string][]byte{
		"not json":       []byte("hello"),
		"other app":      []byte(strings.Replace(string(notificationBody("messages", "0123456789abcdef0123")), "5503910054141466", "1", 1)),
		"unknown topic":  notificationBody("catalog_suggestions", "/x"),
		"resource shape": notificationBody("questions", "/items/1"),
		"no seller":      []byte(`{"_id":"n","resource":"/questions/1","topic":"questions","application_id":5503910054141466}`),
	} {
		if err := accept.Accept(body); !errors.Is(err, ErrNotificationRefused) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
	if queue.total() != 0 || metrics.count("notify/refused_unknown_topic") != 1 || metrics.count("notify/refused_application") != 1 {
		t.Fatalf("published=%d metrics=%v", queue.total(), metrics.events)
	}
	queue.err = errors.New("broker down")
	if err := accept.Accept(notificationBody("messages", "0123456789abcdef0123")); !errors.Is(err, ErrQueueUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestReconciliationTurnsUnreadThreadsIntoTheSameJobs(t *testing.T) {
	accounts := newFakeAccounts()
	a := connectedAccount()
	accounts.put(a)
	b := connectedAccount()
	b.ID, b.MLUserID = "acct-2", "2"
	recent := testNow.Add(-time.Minute)
	b.LastReconciledAt = &recent
	accounts.put(b)
	messages := &fakeMessages{unread: []mldomain.UnreadThread{{PackKey: packKey, SellerID: sellerID, Count: 2}, {PackKey: "2000000000000009", SellerID: "999", Count: 1}}}
	queue := newFakePublisher()
	metrics := newFakeMetrics()
	tokens := NewTokenSource(accounts, &fakeOAuth{refresh: rotated}, fakeClassifier{}, &fakeAlerts{}, metrics)
	tokens.now = fixedClock
	publisher := NewJobPublisher(queue)
	rec := NewReconciler(accounts, tokens, metrics, NewUnreadThreadsReconciler(messages, publisher))
	rec.now = fixedClock

	if err := rec.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	jobs := queue.published[webhook.TopicMercadoLivreMessage]
	if len(jobs) != 1 {
		t.Fatalf("jobs = %d", len(jobs))
	}
	var job mldomain.Job
	_ = json.Unmarshal(jobs[0], &job)
	if job.Kind != mldomain.JobFetchThread || job.ResourceID != packKey || job.SellerID != sellerID || job.Marker == "" {
		t.Fatalf("job = %+v", job)
	}
	if got := accounts.get("acct-1"); got.LastReconciledAt == nil || !got.LastReconciledAt.Equal(testNow) {
		t.Fatalf("not marked: %+v", got.LastReconciledAt)
	}
	if got := accounts.get("acct-2"); !got.LastReconciledAt.Equal(recent) {
		t.Fatal("an account reconciled minutes ago is not due")
	}
}

func TestAFailingAccountIsStillMarkedSoItCannotStarveTheOthers(t *testing.T) {
	accounts := newFakeAccounts()
	accounts.put(connectedAccount())
	messages := &fakeMessages{unreadErr: &mlErr{msg: "down", retryable: true}}
	metrics := newFakeMetrics()
	tokens := NewTokenSource(accounts, &fakeOAuth{refresh: rotated}, fakeClassifier{}, &fakeAlerts{}, metrics)
	tokens.now = fixedClock
	rec := NewReconciler(accounts, tokens, metrics, NewUnreadThreadsReconciler(messages, NewJobPublisher(newFakePublisher())))
	rec.now = fixedClock
	if err := rec.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := accounts.get("acct-1"); got.LastReconciledAt == nil || metrics.count("reconcile/failed") != 1 {
		t.Fatalf("marked=%v metrics=%v", got.LastReconciledAt, metrics.events)
	}
}

func TestMissedFeedsReplayThroughTheSamePipeline(t *testing.T) {
	accounts := newFakeAccounts()
	accounts.put(connectedAccount())
	page := func(ids ...string) []mldomain.Notification {
		var out []mldomain.Notification
		for _, id := range ids {
			out = append(out, mldomain.Notification{ID: id, Resource: "0123456789abcdef" + id, UserID: sellerID, Topic: "messages", ApplicationID: "5503910054141466"})
		}
		return out
	}
	full := make([]string, 100)
	for i := range full {
		full[i] = fmt.Sprintf("%04d", i)
	}
	feeds := &fakeMissedFeeds{pages: map[mldomain.Topic][][]mldomain.Notification{
		mldomain.TopicMessages: {page(full...), page("9999")},
		mldomain.TopicOrders:   {{{ID: "bad", Resource: "/orders/x", UserID: sellerID, Topic: "orders_v2", ApplicationID: "5503910054141466"}}},
	}}
	queue := newFakePublisher()
	metrics := newFakeMetrics()
	tokens := NewTokenSource(accounts, &fakeOAuth{refresh: rotated}, fakeClassifier{}, &fakeAlerts{}, metrics)
	tokens.now = fixedClock
	job := NewMissedFeedsJob(accounts, tokens, feeds, NewAcceptNotification("5503910054141466", testPathToken, NewJobPublisher(queue), metrics))
	job.now = fixedClock
	if err := job.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := len(queue.published[webhook.TopicMercadoLivreMessage]); got != 101 {
		t.Fatalf("replayed = %d", got)
	}
	if metrics.count("notify/refused_resource") != 1 {
		t.Fatalf("metrics = %v", metrics.events)
	}
	for _, q := range feeds.calls {
		if q.AppID != "5503910054141466" || q.Limit != mldomain.MaxMissedFeedLimit {
			t.Fatalf("query = %+v", q)
		}
	}
}
