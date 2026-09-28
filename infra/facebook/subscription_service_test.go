package facebook

import (
	"context"
	"net/http"
	"testing"

	"vozko/infra/meta"
	"vozko/infra/meta/metatest"
)

func newSubscription(t *testing.T, handler http.HandlerFunc) *subscriptionService {
	t.Helper()
	client, err := meta.NewClient(meta.Config{Host: GraphHost, APIVersion: "v25.0", HTTPClient: metatest.Server(t, handler)})
	if err != nil {
		t.Fatal(err)
	}
	return &subscriptionService{client: client}
}

func TestSubscribeVerifiesTheActiveFields(t *testing.T) {
	var posted string
	svc := newSubscription(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			posted = r.URL.Query().Get("subscribed_fields")
			_, _ = w.Write([]byte(`{"success":true}`))
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"data":[{"id":"app","subscribed_fields":["messages","feed"]}]}`))
		}
	})
	active, err := svc.Subscribe(context.Background(), "123", "pt", []string{"messages", "feed"})
	if err != nil {
		t.Fatal(err)
	}
	if posted != "messages,feed" || len(active) != 2 {
		t.Fatalf("posted=%q active=%v", posted, active)
	}
}

func TestSubscribeRefusesInvalidFieldsWithoutCallingMeta(t *testing.T) {
	called := false
	svc := newSubscription(t, func(http.ResponseWriter, *http.Request) { called = true })
	if _, err := svc.Subscribe(context.Background(), "123", "pt", []string{"messages", "message_edit"}); err == nil {
		t.Fatal("invalid field accepted")
	}
	if called {
		t.Fatal("meta was called with an invalid field list")
	}
}

func TestSubscribeRequiresAcknowledgement(t *testing.T) {
	svc := newSubscription(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":false}`))
	})
	if _, err := svc.Subscribe(context.Background(), "123", "pt", []string{"messages"}); err == nil {
		t.Fatal("unacknowledged subscription accepted")
	}
}

func TestUnsubscribeRequiresBothAcknowledgements(t *testing.T) {
	svc := newSubscription(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"messaging_success":false}`))
	})
	if err := svc.Unsubscribe(context.Background(), "123", "pt"); err == nil {
		t.Fatal("partial unsubscribe reported as success")
	}
}
