package emailwebhook

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/emailapi"
	es "vozko/domain/emailsending"
)

func TestEventTypeOfReportsEveryCustomerEventAndSkipsBots(t *testing.T) {
	cases := []struct {
		ev   es.DeliveryEvent
		want EventType
		ok   bool
	}{
		{es.DeliveryEvent{Kind: es.EventSend}, EventSent, true},
		{es.DeliveryEvent{Kind: es.EventDelivery}, EventDelivered, true},
		{es.DeliveryEvent{Kind: es.EventDeliveryDelay}, EventDeliveryDelayed, true},
		{es.DeliveryEvent{Kind: es.EventBounce}, EventBounced, true},
		{es.DeliveryEvent{Kind: es.EventComplaint}, EventComplained, true},
		{es.DeliveryEvent{Kind: es.EventOpen}, EventOpened, true},
		{es.DeliveryEvent{Kind: es.EventClick}, EventClicked, true},
		{es.DeliveryEvent{Kind: es.EventReject}, EventFailed, true},
		{es.DeliveryEvent{Kind: es.EventRenderingFailure}, EventFailed, true},
		{es.DeliveryEvent{Kind: es.EventOpen, IsBot: true}, "", false},
		{es.DeliveryEvent{Kind: es.EventClick, IsBot: true}, "", false},
		{es.DeliveryEvent{Kind: "mystery"}, "", false},
	}
	for _, c := range cases {
		got, ok := EventTypeOf(c.ev)
		if got != c.want || ok != c.ok {
			t.Fatalf("EventTypeOf(%+v) = %q %v, want %q %v", c.ev, got, ok, c.want, c.ok)
		}
	}
	if len(EventTypes) != 8 {
		t.Fatalf("event types = %v", EventTypes)
	}
}

func TestEventIDIsStableAndDistinct(t *testing.T) {
	at := time.Date(2026, 10, 22, 13, 0, 0, 0, time.UTC)
	a := EventID("m-1", es.EventClick, at, "https://x.com/a")
	if a != EventID("m-1", es.EventClick, at, "https://x.com/a") || !strings.HasPrefix(a, "evt_") || len(a) != 36 {
		t.Fatalf("event id = %q", a)
	}
	if a == EventID("m-1", es.EventClick, at, "https://x.com/b") || a == EventID("m-1", es.EventOpen, at, "") {
		t.Fatal("distinct events share an id")
	}
}

func TestValidateURL(t *testing.T) {
	good, err := ValidateURL(" https://hooks.example.com/vozko?x=1 ")
	if err != nil || good != "https://hooks.example.com/vozko?x=1" {
		t.Fatalf("ValidateURL = %q %v", good, err)
	}
	for _, bad := range []string{"", "http://hooks.example.com", "https://", "https://user:pw@x.com/a", "https://x.com/a#frag", "ftp://x.com", "https://x.com/" + strings.Repeat("a", 2100), "https:// x.com"} {
		if _, err := ValidateURL(bad); !errors.Is(err, ErrURLInvalid) {
			t.Fatalf("ValidateURL(%q) = %v", bad, err)
		}
	}
}

func TestValidateEvents(t *testing.T) {
	got, err := ValidateEvents([]string{"email.opened", "email.delivered", "email.opened"})
	if err != nil || len(got) != 2 || got[0] != EventDelivered {
		t.Fatalf("ValidateEvents = %v %v", got, err)
	}
	for _, bad := range [][]string{nil, {"email.nope"}} {
		if _, err := ValidateEvents(bad); !errors.Is(err, ErrEventsInvalid) {
			t.Fatalf("ValidateEvents(%v) = %v", bad, err)
		}
	}
	e := Endpoint{Events: []EventType{EventBounced}, Enabled: true}
	if !e.Wants(EventBounced) || e.Wants(EventOpened) {
		t.Fatal("wants")
	}
	e.Enabled = false
	if e.Wants(EventBounced) {
		t.Fatal("a paused endpoint wants nothing")
	}
}

func TestBackoffGrowsAndEnds(t *testing.T) {
	want := []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute, time.Hour, 3 * time.Hour, 6 * time.Hour}
	for i, w := range want {
		got, ok := Backoff(i + 1)
		if !ok || got != w {
			t.Fatalf("Backoff(%d) = %v %v, want %v", i+1, got, ok, w)
		}
	}
	if _, ok := Backoff(MaxAttempts); ok {
		t.Fatal("the last attempt has no retry")
	}
}

func TestAfterAttempt(t *testing.T) {
	now := time.Date(2026, 10, 22, 13, 0, 0, 0, time.UTC)
	d := Delivery{Attempts: 1}
	if out := d.AfterAttempt(true, now); out.Status != StatusDelivered || out.NextAttemptAt != nil {
		t.Fatalf("success = %+v", out)
	}
	out := d.AfterAttempt(false, now)
	if out.Status != StatusRetrying || out.NextAttemptAt == nil || !out.NextAttemptAt.Equal(now.Add(30*time.Second)) {
		t.Fatalf("first failure = %+v", out)
	}
	last := Delivery{Attempts: MaxAttempts}
	if out := last.AfterAttempt(false, now); out.Status != StatusFailed || out.NextAttemptAt != nil {
		t.Fatalf("last failure = %+v", out)
	}
	if !(Delivery{Status: StatusFailed}).Retryable() || (Delivery{Status: StatusDelivered}).Retryable() || !(Delivery{Status: StatusSkipped}).Retryable() {
		t.Fatal("manual retry rule")
	}
}

func TestSignatureKnownVectorAndVerify(t *testing.T) {
	body := []byte(`{"id":"evt_1"}`)
	header := SignatureHeader("whsec_test", 1761140000, body)
	if header != "t=1761140000,v1=a461ab225263aad0042d1bebe51e1e5cd495133217d44bfc3c3b1e8c7ee4cfc2" {
		t.Fatalf("header = %q", header)
	}
	at := time.Unix(1761140000, 0).Add(time.Minute)
	if err := Verify("whsec_test", header, body, at); err != nil {
		t.Fatalf("verify = %v", err)
	}
	if err := Verify("whsec_other", header, body, at); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("wrong secret = %v", err)
	}
	if err := Verify("whsec_test", header, append(body, ' '), at); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("tampered body = %v", err)
	}
	if err := Verify("whsec_test", header, body, at.Add(10*time.Minute)); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("stale timestamp = %v", err)
	}
}

func TestGenerateSecret(t *testing.T) {
	s, err := GenerateSecret(bytes.NewReader(bytes.Repeat([]byte{9}, 32)))
	if err != nil || !strings.HasPrefix(s, SecretPrefix) || len(s) != len(SecretPrefix)+43 {
		t.Fatalf("secret = %q %v", s, err)
	}
	if Hint(s) != s[len(s)-4:] {
		t.Fatalf("hint = %q", Hint(s))
	}
	if _, err := GenerateSecret(bytes.NewReader(nil)); err == nil {
		t.Fatal("an empty random source must fail")
	}
}

func TestPayloadCarriesTheSendAndTheEventDetail(t *testing.T) {
	at := time.Date(2026, 10, 22, 13, 0, 0, 0, time.UTC)
	entry := emailapi.LogEntry{
		Message: emailapi.Message{ID: "row-1", SendID: "send-1", Address: "ana@example.com", Role: emailapi.RoleCc},
		Send:    emailapi.Send{ID: "send-1", IdempotencyKey: "k-1", From: "no-reply@acme.com", Subject: "Oi", Tags: map[string]string{"kind": "reset"}},
	}
	ev := es.DeliveryEvent{Kind: es.EventBounce, OccurredAt: at, Bounce: &es.Bounce{Kind: es.BounceHard, SubType: "General", Diagnostic: "550"}}
	body, err := BuildPayload("evt_1", EventBounced, entry, ev, at.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Data struct {
			MessageID      string            `json:"message_id"`
			SendID         string            `json:"send_id"`
			IdempotencyKey string            `json:"idempotency_key"`
			To             string            `json:"to"`
			Role           string            `json:"role"`
			Tags           map[string]string `json:"tags"`
			Bounce         *struct {
				Type string `json:"type"`
			} `json:"bounce"`
			Click *struct{} `json:"click"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ID != "evt_1" || decoded.Type != "email.bounced" || decoded.Data.MessageID != "row-1" || decoded.Data.SendID != "send-1" ||
		decoded.Data.IdempotencyKey != "k-1" || decoded.Data.To != "ana@example.com" || decoded.Data.Role != "cc" || decoded.Data.Tags["kind"] != "reset" ||
		decoded.Data.Bounce == nil || decoded.Data.Bounce.Type != "hard" || decoded.Data.Click != nil {
		t.Fatalf("payload = %s", body)
	}
}

func TestFailingAlertDue(t *testing.T) {
	now := time.Date(2026, 10, 22, 13, 0, 0, 0, time.UTC)
	since := now.Add(-2 * time.Hour)
	recent := now.Add(-10 * time.Minute)
	old := now.Add(-2 * time.Hour)
	fresh := now.Add(-10 * time.Minute)
	if (Endpoint{}).AlertDue(now) || (Endpoint{FailingSince: &fresh}).AlertDue(now) {
		t.Fatal("no alert before an hour of failure")
	}
	if !(Endpoint{FailingSince: &since}).AlertDue(now) || (Endpoint{FailingSince: &since, AlertedAt: &recent}).AlertDue(now) || !(Endpoint{FailingSince: &since, AlertedAt: &old}).AlertDue(now) {
		t.Fatal("alert at most once an hour after an hour of failure")
	}
}
