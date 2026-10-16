package businessphone_usecase

import (
	"errors"
	"testing"
	"time"

	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/optout"
	"vozko/domain/whatsapp/quality"
)

type fakeQualityRecorder struct {
	changes []quality.Change
	err     error
}

func (f *fakeQualityRecorder) Record(changes ...quality.Change) error {
	if f.err != nil {
		return f.err
	}
	f.changes = append(f.changes, changes...)
	return nil
}

func qualityWebhook(at int64, limit, event string) *businessphone.PhoneWebhookPayload {
	return &businessphone.PhoneWebhookPayload{Entry: []businessphone.PhoneWebhookEntry{{
		ID: "waba1", Time: at,
		Changes: []businessphone.PhoneWebhookChange{{
			Field: businessphone.FieldPhoneNumberQualityUpdate,
			Value: businessphone.PhoneWebhookValue{DisplayPhoneNumber: "+15551234567", CurrentLimit: limit, Event: event},
		}},
	}}}
}

func TestPhoneQualityWebhookRecordsTheEventAndTheLimitThatMoved(t *testing.T) {
	repo := newMockRepo()
	seedPhone(repo, "p1", "+15551234567", "waba1")
	recorder := &fakeQualityRecorder{}
	uc := NewHandlePhoneWebhook(repo, newMockWABARepo(), recorder, &fakeOptOutRecorder{})

	if err := uc.Execute(qualityWebhook(1791990878, "TIER_250", "downgrade")); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	at := time.Unix(1791990878, 0).UTC()
	want := []quality.Change{
		{Subject: quality.SubjectPhone, ExternalID: "meta_p1", WABAID: "waba1", Name: "+15551234567", Signal: quality.SignalStatus,
			Event: "DOWNGRADE", Current: "DOWNGRADE", Source: quality.SourceWebhook, OccurredAt: at},
		{Subject: quality.SubjectPhone, ExternalID: "meta_p1", WABAID: "waba1", Name: "+15551234567", Signal: quality.SignalMessagingLimit,
			Event: "DOWNGRADE", Previous: "TIER_1K", Current: "TIER_250", Source: quality.SourceWebhook, OccurredAt: at},
	}
	if len(recorder.changes) != len(want) {
		t.Fatalf("got %+v", recorder.changes)
	}
	for i := range want {
		if recorder.changes[i] != want[i] {
			t.Fatalf("change %d\ngot  %+v\nwant %+v", i, recorder.changes[i], want[i])
		}
	}
}

func TestPhoneQualityWebhookSkipsALimitThatDidNotMove(t *testing.T) {
	repo := newMockRepo()
	seedPhone(repo, "p1", "+15551234567", "waba1")
	recorder := &fakeQualityRecorder{}
	uc := NewHandlePhoneWebhook(repo, newMockWABARepo(), recorder, &fakeOptOutRecorder{})

	if err := uc.Execute(qualityWebhook(1791990878, "TIER_1K", "FLAGGED")); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(recorder.changes) != 1 || recorder.changes[0].Signal != quality.SignalStatus {
		t.Fatalf("only the FLAGGED event is new, got %+v", recorder.changes)
	}
}

func TestPhoneQualityWebhookReturnsAFailedRecording(t *testing.T) {
	boom := errors.New("db down")
	repo := newMockRepo()
	seedPhone(repo, "p1", "+15551234567", "waba1")
	uc := NewHandlePhoneWebhook(repo, newMockWABARepo(), &fakeQualityRecorder{err: boom}, &fakeOptOutRecorder{})

	err := uc.Execute(qualityWebhook(1791990878, "TIER_10K", "FLAGGED"))

	if !errors.Is(err, boom) {
		t.Fatalf("want the recording error so the event is retried, got %v", err)
	}
	if repo.phoneNumbers["p1"].Status != businessphone.StatusFlagged {
		t.Fatal("the phone itself is still updated")
	}
}

func TestPhoneQualityWebhookDoesNotLoopOnAPhoneWithoutMetaID(t *testing.T) {
	repo := newMockRepo()
	phone := seedPhone(repo, "p1", "+15551234567", "waba1")
	phone.MetaPhoneNumberID = ""
	recorder := &fakeQualityRecorder{err: errors.New("must not be called")}
	uc := NewHandlePhoneWebhook(repo, newMockWABARepo(), recorder, &fakeOptOutRecorder{})

	if err := uc.Execute(qualityWebhook(1791990878, "TIER_10K", "FLAGGED")); err != nil {
		t.Fatalf("a phone that cannot be identified must not make the event retry forever, got %v", err)
	}
	if repo.phoneNumbers["p1"].Status != businessphone.StatusFlagged {
		t.Fatal("the phone itself is still updated")
	}
}

type fakeOptOutRecorder struct {
	events []optout.Event
	err    error
}

func (f *fakeOptOutRecorder) Record(events ...optout.Event) error {
	if f.err != nil {
		return f.err
	}
	f.events = append(f.events, events...)
	return nil
}

func preferencesWebhook(at int64, prefs ...businessphone.PhoneUserPreference) *businessphone.PhoneWebhookPayload {
	value := businessphone.PhoneWebhookValue{UserPreferences: prefs}
	value.Metadata.PhoneNumberID = "1179270238614202"
	return &businessphone.PhoneWebhookPayload{Entry: []businessphone.PhoneWebhookEntry{{
		ID: "waba1", Time: at,
		Changes: []businessphone.PhoneWebhookChange{{Field: optout.WebhookField, Value: value}},
	}}}
}

func TestUserPreferencesAreRecordedWithoutTheRecipientNumber(t *testing.T) {
	optOuts := &fakeOptOutRecorder{}
	uc := NewHandlePhoneWebhook(newMockRepo(), newMockWABARepo(), &fakeQualityRecorder{}, optOuts)

	err := uc.Execute(preferencesWebhook(1791990000,
		businessphone.PhoneUserPreference{WaID: "5511999990001", Category: "marketing_messages", Value: "stop", Timestamp: 1791990878},
		businessphone.PhoneUserPreference{WaID: "5511999990002", Category: "marketing_messages", Value: "resume"},
		businessphone.PhoneUserPreference{WaID: "", Category: "marketing_messages", Value: "stop", Timestamp: 1},
		businessphone.PhoneUserPreference{WaID: "5511999990003", Category: "marketing_messages", Value: "pause", Timestamp: 1},
	))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	want := []optout.Event{
		{PhoneNumberID: "1179270238614202", RecipientHash: optout.HashRecipient("5511999990001"), Category: "marketing_messages", Preference: optout.PreferenceStop, OccurredAt: time.Unix(1791990878, 0).UTC()},
		{PhoneNumberID: "1179270238614202", RecipientHash: optout.HashRecipient("5511999990002"), Category: "marketing_messages", Preference: optout.PreferenceResume, OccurredAt: time.Unix(1791990000, 0).UTC()},
	}
	if len(optOuts.events) != len(want) {
		t.Fatalf("only valid preferences are recorded, got %+v", optOuts.events)
	}
	for i := range want {
		if optOuts.events[i] != want[i] {
			t.Fatalf("event %d\ngot  %+v\nwant %+v", i, optOuts.events[i], want[i])
		}
	}
}

func TestAFailedOptOutRecordingIsRetried(t *testing.T) {
	boom := errors.New("db down")
	uc := NewHandlePhoneWebhook(newMockRepo(), newMockWABARepo(), &fakeQualityRecorder{}, &fakeOptOutRecorder{err: boom})

	err := uc.Execute(preferencesWebhook(1791990000, businessphone.PhoneUserPreference{WaID: "5511999990001", Value: "stop", Timestamp: 1791990878}))

	if !errors.Is(err, boom) {
		t.Fatalf("want the recording error so the event is retried, got %v", err)
	}
}
