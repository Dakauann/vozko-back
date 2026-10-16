package conversation_usecase

import (
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/whatsapp/messagestatus"
)

type fakeStatusRecorder struct {
	events []messagestatus.Event
	err    error
	calls  int
}

func (f *fakeStatusRecorder) Record(events ...messagestatus.Event) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	f.events = append(f.events, events...)
	return nil
}

func statusPayload(phoneNumberID string, statuses ...conversation.WhatsAppStatus) *conversation.WhatsAppWebhookPayload {
	payload := &conversation.WhatsAppWebhookPayload{}
	payload.Entry = append(payload.Entry, conversation.WhatsAppEntry{})
	payload.Entry[0].Changes = append(payload.Entry[0].Changes, conversation.WhatsAppChange{})
	payload.Entry[0].Changes[0].Value.Metadata.PhoneNumberID = phoneNumberID
	payload.Entry[0].Changes[0].Value.Statuses = statuses
	return payload
}

func TestStatusWebhookRecordsEveryDeliveryStatusInOneBatch(t *testing.T) {
	recorder := &fakeStatusRecorder{}
	uc := &handleWhatsAppMessageUseCase{statusEvents: recorder}

	err := uc.logStatusUpdates(statusPayload("1179270238614202",
		conversation.WhatsAppStatus{ID: "wamid.A", Status: "delivered", Timestamp: "1791986489", Pricing: &conversation.WhatsAppPricing{Category: "utility"}},
		conversation.WhatsAppStatus{ID: "wamid.B", Status: "failed", Timestamp: "1791986490", Errors: []conversation.WhatsAppError{{Code: 131026}}},
		conversation.WhatsAppStatus{ID: "wamid.C", Status: "deleted", Timestamp: "1791986491"},
	))

	if err != nil {
		t.Fatalf("logStatusUpdates: %v", err)
	}
	if recorder.calls != 1 {
		t.Fatalf("one insert per webhook, got %d", recorder.calls)
	}
	want := []messagestatus.Event{
		{MessageID: "wamid.A", PhoneNumberID: "1179270238614202", Status: messagestatus.StatusDelivered, Pricing: "utility", OccurredAt: time.Unix(1791986489, 0).UTC()},
		{MessageID: "wamid.B", PhoneNumberID: "1179270238614202", Status: messagestatus.StatusFailed, ErrorCode: 131026, OccurredAt: time.Unix(1791986490, 0).UTC()},
	}
	if len(recorder.events) != len(want) {
		t.Fatalf("an unknown status is not recorded, got %+v", recorder.events)
	}
	for i := range want {
		if recorder.events[i] != want[i] {
			t.Fatalf("event %d\ngot  %+v\nwant %+v", i, recorder.events[i], want[i])
		}
	}
}

func TestStatusWebhookIsRetriedWhenRecordingFails(t *testing.T) {
	boom := errors.New("db down")
	uc := &handleWhatsAppMessageUseCase{statusEvents: &fakeStatusRecorder{err: boom}}

	err := uc.logStatusUpdates(statusPayload("1179270238614202",
		conversation.WhatsAppStatus{ID: "wamid.A", Status: "read", Timestamp: "1791986489"},
	))

	if !errors.Is(err, conversation.ErrWhatsAppWebhookRetryable) || !strings.Contains(err.Error(), boom.Error()) {
		t.Fatalf("a lost status must make the webhook retry, got %v", err)
	}
}

func TestStatusWithoutTheSendingNumberIsNotRecordedSoItCannotBlockTheWebhook(t *testing.T) {
	recorder := &fakeStatusRecorder{}
	uc := &handleWhatsAppMessageUseCase{statusEvents: recorder}

	if err := uc.logStatusUpdates(statusPayload("", conversation.WhatsAppStatus{ID: "wamid.A", Status: "read", Timestamp: "1791986489"})); err != nil {
		t.Fatalf("logStatusUpdates: %v", err)
	}
	if len(recorder.events) != 0 {
		t.Fatalf("an event that can never be attributed is skipped, got %+v", recorder.events)
	}
}

func TestStatusEventFallsBackToNowWhenMetaSendsNoTimestamp(t *testing.T) {
	now := time.Date(2026, 10, 15, 20, 0, 0, 0, time.UTC)
	event, ok := statusEventOf("1179270238614202", conversation.WhatsAppStatus{ID: "wamid.A", Status: "sent"}, now)
	if !ok || !event.OccurredAt.Equal(now) {
		t.Fatalf("got %+v, %v", event, ok)
	}
}
