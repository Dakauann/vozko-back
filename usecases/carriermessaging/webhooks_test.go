package carriermessaging_usecase

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/carriermessaging"
	"vozko/domain/rcs"
	"vozko/domain/sms"
	webhook_usecase "vozko/usecases/webhook"
)

func TestWebhookDedupKeys(t *testing.T) {
	status := StatusDedupKey(&carriermessaging.StatusReport{Reference: "ref", Status: carriermessaging.StatusDelivered})
	if status != "carrier:status:ref:delivered:" {
		t.Fatalf("status key = %q", status)
	}
	if StatusDedupKey(&carriermessaging.StatusReport{Reference: "ref", Status: carriermessaging.StatusFailed, ErrorCode: "42"}) == status {
		t.Fatal("a different outcome of the same message is a different report")
	}
	if InboundDedupKey(&carriermessaging.Inbound{Channel: carriermessaging.ChannelSMS, ProviderMessageID: "mo-1"}) != "carrier:inbound:sms:mo-1" {
		t.Fatal("inbound key")
	}
	if EventDedupKey(&carriermessaging.SuggestionClick{EventID: "ev-1"}) != "carrier:event:ev-1" {
		t.Fatal("event key")
	}
	if StatusDedupKey(nil) != "" || InboundDedupKey(&carriermessaging.Inbound{}) != "" || EventDedupKey(&carriermessaging.SuggestionClick{}) != "" {
		t.Fatal("an empty payload has no key, so the runner never dedups it by accident")
	}
}

func TestWebhookFailureClassification(t *testing.T) {
	drop := []error{
		carriermessaging.ErrMessageNotFound, rcs.ErrSenderNotFound, sms.ErrInboundUnroutable,
		carriermessaging.ErrPhoneInvalid, ErrButtonUnknown, ErrChannelInvalid,
	}
	for _, err := range drop {
		if got := ClassifyWebhookFailure(err); got != webhook_usecase.DispositionDrop {
			t.Fatalf("%v = %v, want drop", err, got)
		}
	}
	if got := ClassifyWebhookFailure(errors.New("database down")); got != webhook_usecase.DispositionRetry {
		t.Fatalf("transient = %v, want retry", got)
	}
}

func TestQueuedEventsRoundTripThroughJSON(t *testing.T) {
	in := carriermessaging.Inbound{
		ProviderMessageID: "mo-1", Channel: carriermessaging.ChannelSMS, Phone: "5511999990000", To: "29000",
		Text: "Oi", Media: &carriermessaging.InboundMedia{URL: "https://a.example.com/x.jpg", MIMEType: "image/jpeg"},
		ReceivedAt: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC),
	}
	raw, err := EncodeQueued(in)
	if err != nil {
		t.Fatalf("EncodeQueued = %v", err)
	}
	var out carriermessaging.Inbound
	if err := DecodeQueued(raw, &out); err != nil || out.Media == nil || out.Media.URL != in.Media.URL || !out.ReceivedAt.Equal(in.ReceivedAt) || out.Channel != in.Channel {
		t.Fatalf("round trip = %+v, %v", out, err)
	}
	if err := DecodeQueued([]byte("{"), &out); err == nil {
		t.Fatal("a broken payload is an error")
	}
}
