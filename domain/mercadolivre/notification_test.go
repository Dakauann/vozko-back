package mercadolivre

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func loadNotification(t *testing.T, name string) Notification {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	n, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode(%s): %v", name, err)
	}
	return n
}

func TestNormalizeDocumentedNotifications(t *testing.T) {
	cases := []struct {
		fixture  string
		appID    string
		kind     JobKind
		seller   string
		resource string
		read     bool
		key      string
	}{
		{"notification_questions_doc_example.json", "2069392825111111", JobFetchQuestion, "123456789", "5036111111", false,
			"ml:questions:/questions/5036111111:r:2017-10-09T13:51:05.438Z"},
		{"notification_messages_doc_example.json", "89745685555", JobFetchMessage, "123456789", "3f6da1e35ac84f70a24af7360d24c7bc", false,
			"ml:messages:3f6da1e35ac84f70a24af7360d24c7bc:5e2827f2-99b7-474e-b68b-6a86e934cc7e"},
		{"notification_messages_read_doc_example.json", "89745685555", JobFetchMessage, "123456789", "3f6da1e35ac84f70a24af7360d24c7bc", true,
			"ml:messages:3f6da1e35ac84f70a24af7360d24c7bc:5e2827f2-99b7-474e-b68b-6a86e934cc7e"},
		{"notification_messages_missed_feed_doc_example.json", "3486171129139063", JobFetchMessage, "465432224", "a35cf79864a845ca9a3bf6aee59bb4d7", false,
			"ml:messages:a35cf79864a845ca9a3bf6aee59bb4d7:r:2019-10-17T17:15:30.259Z"},
		{"notification_orders_v2_doc_example.json", "5503910054141466", JobFetchOrder, "468424240", "2195160686", false,
			"ml:orders_v2:/orders/2195160686:r:2019-10-30T16:19:20.106Z"},
		{"notification_items_mlb_from_doc_example.json", "2069392825111111", JobFetchItem, "123456789", "MLB3737188005", false,
			"ml:items:/items/MLB3737188005:f9f08571-1f65-4c46-9e0a-c0f43faa1557e"},
		{"notification_shipments_constructed.json", "213123389095511", JobFetchShipment, "779432305", "43729529445", false,
			"ml:shipments:/shipments/43729529445:495cac10-8496-45f8-a6f5-8bff0a948597"},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			job, err := Normalize(loadNotification(t, tc.fixture), tc.appID)
			if err != nil {
				t.Fatalf("Normalize: %v", err)
			}
			if job.Kind != tc.kind || job.SellerID != tc.seller || job.ResourceID != tc.resource || job.Read != tc.read {
				t.Fatalf("job = %+v", job)
			}
			if got := job.IdempotencyKey(); got != tc.key {
				t.Fatalf("IdempotencyKey = %q, want %q", got, tc.key)
			}
		})
	}
}

func TestDecodeToleratesTheDocumentedShapeVariations(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		userID   string
		appID    string
		id       string
		received string
	}{
		{"user id as a string", `{"user_id":"123","application_id":5,"topic":"questions","resource":"/questions/1","received":"r1"}`, "123", "5", "", "r1"},
		{"user id as a number", `{"user_id":123,"application_id":"5","topic":"questions","resource":"/questions/1"}`, "123", "5", "", ""},
		{"misspelled received", `{"user_id":1,"application_id":5,"recieved":"2021-12-05T17:13:53Z"}`, "1", "5", "", "2021-12-05T17:13:53Z"},
		{"underscore id", `{"_id":"abc","user_id":1,"application_id":5}`, "1", "5", "abc", ""},
		{"plain id", `{"id":"def","user_id":1,"application_id":5}`, "1", "5", "def", ""},
		{"application id beyond float precision", `{"user_id":1,"application_id":1111111111111111111}`, "1", "1111111111111111111", "", ""},
		{"null user id", `{"user_id":null,"application_id":5}`, "", "5", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, err := Decode([]byte(tc.raw))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if n.UserID != tc.userID || n.ApplicationID != tc.appID || n.ID != tc.id || n.Received != tc.received {
				t.Fatalf("notification = %+v", n)
			}
		})
	}
	for _, raw := range []string{``, `[]`, `{"user_id":1.5}`, `{"user_id":true}`, `{"user_id":"12a"}`, `{"application_id":-3}`, `not json`} {
		if _, err := Decode([]byte(raw)); !errors.Is(err, ErrInvalidNotification) {
			t.Errorf("Decode(%q) err = %v, want ErrInvalidNotification", raw, err)
		}
	}
}

func TestDecodeKeepsTheMisspelledReceivedOfTheDocExamples(t *testing.T) {
	n := loadNotification(t, "notification_catalog_suggestions_doc_example.json")
	if n.Received != "2021-12-05T17:13:53.617074685Z" || n.Topic != "catalog_suggestions" {
		t.Fatalf("notification = %+v", n)
	}
}

func TestNormalizeRefusals(t *testing.T) {
	cases := []struct {
		name  string
		n     Notification
		appID string
		err   error
	}{
		{"foreign application", Notification{ApplicationID: "999", UserID: "1", Topic: "questions", Resource: "/questions/1", Received: "r"}, "5", ErrWrongApplication},
		{"application not configured", Notification{ApplicationID: "5", UserID: "1", Topic: "questions", Resource: "/questions/1", Received: "r"}, "", ErrWrongApplication},
		{"missing user", Notification{ApplicationID: "5", Topic: "questions", Resource: "/questions/1", Received: "r"}, "5", ErrUserIDInvalid},
		{"unknown topic", Notification{ApplicationID: "5", UserID: "1", Topic: "payments", Resource: "/collections/1", Received: "r"}, "5", ErrUnknownTopic},
		{"empty topic", Notification{ApplicationID: "5", UserID: "1", Resource: "/questions/1", Received: "r"}, "5", ErrUnknownTopic},
		{"question with text id", Notification{ApplicationID: "5", UserID: "1", Topic: "questions", Resource: "/questions/abc", Received: "r"}, "5", ErrInvalidResource},
		{"question with a trailing path", Notification{ApplicationID: "5", UserID: "1", Topic: "questions", Resource: "/questions/1/answers", Received: "r"}, "5", ErrInvalidResource},
		{"question without leading slash", Notification{ApplicationID: "5", UserID: "1", Topic: "questions", Resource: "questions/1", Received: "r"}, "5", ErrInvalidResource},
		{"message as a path", Notification{ApplicationID: "5", UserID: "1", Topic: "messages", Resource: "/messages/3f6da1e35ac84f70a24af7360d24c7bc", Received: "r"}, "5", ErrInvalidResource},
		{"message with symbols", Notification{ApplicationID: "5", UserID: "1", Topic: "messages", Resource: "3f6da1e35ac84f70-a24af7360d24c7bc", Received: "r"}, "5", ErrInvalidResource},
		{"message too short", Notification{ApplicationID: "5", UserID: "1", Topic: "messages", Resource: "abc", Received: "r"}, "5", ErrInvalidResource},
		{"message with an unknown action", Notification{ApplicationID: "5", UserID: "1", Topic: "messages", Resource: "3f6da1e35ac84f70a24af7360d24c7bc", Actions: []string{"deleted"}, Received: "r"}, "5", ErrUnknownAction},
		{"order with text", Notification{ApplicationID: "5", UserID: "1", Topic: "orders_v2", Resource: "/orders/x1", Received: "r"}, "5", ErrInvalidResource},
		{"order under the wrong topic", Notification{ApplicationID: "5", UserID: "1", Topic: "shipments", Resource: "/orders/1", Received: "r"}, "5", ErrInvalidResource},
		{"item of another site", Notification{ApplicationID: "5", UserID: "1", Topic: "items", Resource: "/items/MLA686791111", Received: "r"}, "5", ErrInvalidResource},
		{"item price path", Notification{ApplicationID: "5", UserID: "1", Topic: "items", Resource: "/items/MLB1/price_to_win", Received: "r"}, "5", ErrInvalidResource},
		{"no identity to deduplicate on", Notification{ApplicationID: "5", UserID: "1", Topic: "questions", Resource: "/questions/1"}, "5", ErrNotificationIdentity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Normalize(tc.n, tc.appID); !errors.Is(err, tc.err) {
				t.Fatalf("Normalize err = %v, want %v", err, tc.err)
			}
		})
	}
}

func TestDocumentedNotificationsWeDoNotHandleAreRefused(t *testing.T) {
	cases := []struct {
		fixture string
		appID   string
		err     error
	}{
		{"notification_items_doc_example.json", "2069392825111111", ErrInvalidResource},
		{"notification_catalog_suggestions_doc_example.json", "5775857146034005", ErrUnknownTopic},
		{"notification_public_offers_doc_example.json", "5111111111111", ErrUnknownTopic},
	}
	for _, tc := range cases {
		if _, err := Normalize(loadNotification(t, tc.fixture), tc.appID); !errors.Is(err, tc.err) {
			t.Errorf("%s: err = %v, want %v", tc.fixture, err, tc.err)
		}
	}
}

func TestIdempotencyKeyFallsBackFromIDToReceivedToSent(t *testing.T) {
	base := Notification{ApplicationID: "5", UserID: "1", Topic: "questions", Resource: "/questions/9"}
	withID := base
	withID.ID, withID.Received, withID.Sent = "n1", "r1", "s1"
	withReceived := base
	withReceived.Received, withReceived.Sent = "r1", "s1"
	withSent := base
	withSent.Sent = "s1"
	cases := []struct {
		n    Notification
		want string
	}{
		{withID, "ml:questions:/questions/9:n1"},
		{withReceived, "ml:questions:/questions/9:r:r1"},
		{withSent, "ml:questions:/questions/9:s:s1"},
	}
	for _, tc := range cases {
		job, err := Normalize(tc.n, "5")
		if err != nil {
			t.Fatalf("Normalize: %v", err)
		}
		if got := job.IdempotencyKey(); got != tc.want {
			t.Errorf("IdempotencyKey = %q, want %q", got, tc.want)
		}
	}
}

func TestReconciliationJobs(t *testing.T) {
	thread, err := ThreadJob("378136913", "1977056109", "run-1")
	if err != nil || thread.Kind != JobFetchThread || thread.SellerID != "378136913" || thread.ResourceID != "1977056109" {
		t.Fatalf("ThreadJob = %+v, %v", thread, err)
	}
	if got := thread.IdempotencyKey(); got != "ml:thread:/packs/1977056109/sellers/378136913:m:run-1" {
		t.Fatalf("thread key = %q", got)
	}
	question, err := QuestionJob("378136913", "11436370259", "run-1")
	if err != nil || question.Kind != JobFetchQuestion || question.ResourceID != "11436370259" {
		t.Fatalf("QuestionJob = %+v, %v", question, err)
	}
	if got := question.IdempotencyKey(); got != "ml:questions:/questions/11436370259:m:run-1" {
		t.Fatalf("question key = %q", got)
	}
	refusals := []func() error{
		func() error { _, err := ThreadJob("x", "1", "m"); return err },
		func() error { _, err := ThreadJob("1", "x", "m"); return err },
		func() error { _, err := ThreadJob("1", "2", ""); return err },
		func() error { _, err := QuestionJob("1", "x", "m"); return err },
		func() error { _, err := QuestionJob("", "2", "m"); return err },
		func() error { _, err := QuestionJob("1", "2", " "); return err },
	}
	for i, f := range refusals {
		if f() == nil {
			t.Errorf("refusal %d accepted", i)
		}
	}
}
