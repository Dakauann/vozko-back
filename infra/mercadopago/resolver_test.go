package mercadopago

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"vozko/domain/payment"
)

func notificationJSON(typ, action, dataID string) []byte {
	return []byte(fmt.Sprintf(
		`{"id":112233,"live_mode":true,"type":%q,"action":%q,"api_version":"v1","data":{"id":%q}}`,
		typ, action, dataID))
}

func TestResolver_ProviderIsMercadoPago(t *testing.T) {
	r := NewWebhookResolver(&fakeClient{})
	if r.Provider() != payment.ProviderMercadoPago {
		t.Fatalf("provider: got %q", r.Provider())
	}
}

// TestResolver_FetchesPaymentBeforeDeciding is the defining behaviour: the notification
// carries no state, so the resolver must exchange the id for a payment.
func TestResolver_FetchesPaymentBeforeDeciding(t *testing.T) {
	f := &fakeClient{getResponse: &Payment{
		ID:                1234567890,
		Status:            StatusApproved,
		StatusDetail:      DetailAccredited,
		ExternalReference: "inv:abc",
		PaymentMethodID:   PaymentMethodPix,
		TransactionAmount: 49.9,
	}}
	r := NewWebhookResolver(f)

	event, err := r.Resolve(context.Background(), notificationJSON("payment", "payment.updated", "1234567890"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.getID != "1234567890" {
		t.Fatalf("the payment was not fetched, getID=%q", f.getID)
	}
	if event.Event != payment.EventPaymentReceived {
		t.Fatalf("event: got %q", event.Event)
	}
	if event.Payment.ID != "1234567890" || event.Payment.ExternalReference != "inv:abc" {
		t.Fatalf("event payload: %+v", event.Payment)
	}
	if event.ID != "112233" {
		t.Fatalf("notification id not carried: %q", event.ID)
	}
}

// TestResolver_IsIdempotentUnderRedelivery documents why deriving the event from the
// payment's CURRENT state is the right design: two deliveries resolve identically.
func TestResolver_IsIdempotentUnderRedelivery(t *testing.T) {
	f := &fakeClient{getResponse: &Payment{
		ID: 1, Status: StatusApproved, StatusDetail: DetailAccredited, TransactionAmount: 10,
	}}
	r := NewWebhookResolver(f)
	raw := notificationJSON("payment", "payment.created", "1")

	first, err := r.Resolve(context.Background(), raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// A second delivery labelled with a different action still resolves to the same
	// event, because the payment's state has not moved.
	second, err := r.Resolve(context.Background(), notificationJSON("payment", "payment.updated", "1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first.Event != second.Event {
		t.Fatalf("redelivery resolved differently: %q vs %q", first.Event, second.Event)
	}
}

func TestResolver_IgnoresNonPaymentTopics(t *testing.T) {
	f := &fakeClient{}
	r := NewWebhookResolver(f)

	for _, typ := range []string{"plan", "subscription", "invoice", "point_integration_wh"} {
		_, err := r.Resolve(context.Background(), notificationJSON(typ, "created", "1"))
		if !errors.Is(err, payment.ErrWebhookIgnored) {
			t.Errorf("%q: expected ErrWebhookIgnored, got %v", typ, err)
		}
	}
	if f.getID != "" {
		t.Fatal("a non-payment notification must not trigger an API call")
	}
}

func TestResolver_IgnoresNonActionableState(t *testing.T) {
	// in_process is real and authentic but must move nothing locally.
	f := &fakeClient{getResponse: &Payment{ID: 1, Status: StatusInProcess}}
	r := NewWebhookResolver(f)

	_, err := r.Resolve(context.Background(), notificationJSON("payment", "payment.updated", "1"))
	if errors.Is(err, payment.ErrWebhookIgnored) {
		t.Fatal("in_process should resolve to the informational event, not be dropped")
	}

	// An unmappable status is dropped instead.
	f2 := &fakeClient{getResponse: &Payment{ID: 1, Status: "status_from_the_future"}}
	_, err = NewWebhookResolver(f2).Resolve(context.Background(), notificationJSON("payment", "payment.updated", "1"))
	if !errors.Is(err, payment.ErrWebhookIgnored) {
		t.Fatalf("expected ErrWebhookIgnored for an unknown status, got %v", err)
	}
}

func TestResolver_MalformedPayloadIsTerminal(t *testing.T) {
	r := NewWebhookResolver(&fakeClient{})

	if _, err := r.Resolve(context.Background(), []byte("{not json")); !errors.Is(err, payment.ErrWebhookMalformed) {
		t.Fatalf("expected ErrWebhookMalformed for bad JSON, got %v", err)
	}
	if _, err := r.Resolve(context.Background(), []byte(`{"type":"payment"}`)); !errors.Is(err, payment.ErrWebhookMalformed) {
		t.Fatalf("expected ErrWebhookMalformed for a missing id, got %v", err)
	}
}

func TestResolver_NonNumericIDIsTerminal(t *testing.T) {
	r := NewWebhookResolver(&fakeClient{getErr: ErrInvalidPaymentID})
	_, err := r.Resolve(context.Background(), notificationJSON("payment", "payment.updated", "not-a-number"))
	if !errors.Is(err, payment.ErrWebhookMalformed) {
		t.Fatalf("expected ErrWebhookMalformed, got %v", err)
	}
}

// TestResolver_NotFoundIsDropped: a payment from another account or environment will
// 404 forever, so retrying only fills the queue.
func TestResolver_NotFoundIsDropped(t *testing.T) {
	r := NewWebhookResolver(&fakeClient{getErr: &ResponseError{StatusCode: 404}})
	_, err := r.Resolve(context.Background(), notificationJSON("payment", "payment.updated", "1"))
	if !errors.Is(err, payment.ErrWebhookIgnored) {
		t.Fatalf("expected ErrWebhookIgnored, got %v", err)
	}
}

// TestResolver_UnauthorizedIsRetryable: a rotated token must NOT drop the message, or
// every payment received during the outage is silently lost.
func TestResolver_UnauthorizedIsRetryable(t *testing.T) {
	r := NewWebhookResolver(&fakeClient{getErr: &ResponseError{StatusCode: 401}})
	_, err := r.Resolve(context.Background(), notificationJSON("payment", "payment.updated", "1"))
	if err == nil {
		t.Fatal("expected an error")
	}
	if errors.Is(err, payment.ErrWebhookIgnored) || errors.Is(err, payment.ErrWebhookMalformed) {
		t.Fatalf("an auth failure must stay retryable, got %v", err)
	}
}

func TestResolver_TransientErrorIsRetryable(t *testing.T) {
	r := NewWebhookResolver(&fakeClient{getErr: &ResponseError{StatusCode: 503}})
	_, err := r.Resolve(context.Background(), notificationJSON("payment", "payment.updated", "1"))
	if err == nil {
		t.Fatal("expected an error")
	}
	if errors.Is(err, payment.ErrWebhookIgnored) || errors.Is(err, payment.ErrWebhookMalformed) {
		t.Fatalf("a 5xx must stay retryable, got %v", err)
	}
}

func TestResolver_NilClientIsSafe(t *testing.T) {
	r := NewWebhookResolver(nil)
	if _, err := r.Resolve(context.Background(), notificationJSON("payment", "payment.updated", "1")); err == nil {
		t.Fatal("expected an error with no client configured")
	}
}

func TestResolver_LegacyIPNEnvelope(t *testing.T) {
	// The delivery layer normalizes query-only IPNs into a body before queueing, so
	// the resolver sees the topic form rather than raw query parameters.
	f := &fakeClient{getResponse: &Payment{ID: 77, Status: StatusApproved, TransactionAmount: 5}}
	r := NewWebhookResolver(f)

	event, err := r.Resolve(context.Background(), []byte(`{"topic":"payment","data":{"id":"77"}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Payment.ID != "77" {
		t.Fatalf("charge id: got %q", event.Payment.ID)
	}
	// With no notification id in the envelope, the resource id stands in.
	if event.ID != "77" {
		t.Fatalf("event id fallback: got %q", event.ID)
	}
}
