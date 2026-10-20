package messagestate

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type recordingStore struct {
	saved []string
	fail  error
}

func (s *recordingStore) Update(messageID string, _ *conversation.Message) error {
	if s.fail != nil {
		return s.fail
	}
	s.saved = append(s.saved, messageID)
	return nil
}

type recordingAnnouncer struct {
	events []string
}

func (a *recordingAnnouncer) BroadcastMessageUpdated(entryID, entryType string, message *conversation.Message) {
	a.events = append(a.events, "message_updated:"+entryType+":"+entryID+":"+message.ID)
}

func (a *recordingAnnouncer) BroadcastEntryUpdate(entryID, entryType string, _ *conversation.Message) {
	a.events = append(a.events, "entry_update:"+entryType+":"+entryID)
}

func TestSaveStoresTheMessageThenUpdatesTheThreadAndTheInboxRow(t *testing.T) {
	store, announcer := &recordingStore{}, &recordingAnnouncer{}
	message := &conversation.Message{ID: "m1", EntryID: "e1", EntryType: shared.EntryTypeTelegram}

	if err := (Writer{Messages: store, Announcer: announcer}).Save(message); err != nil {
		t.Fatal(err)
	}

	if len(store.saved) != 1 || store.saved[0] != "m1" {
		t.Fatalf("saved = %v", store.saved)
	}
	want := []string{"message_updated:telegram:e1:m1", "entry_update:telegram:e1"}
	if len(announcer.events) != 2 || announcer.events[0] != want[0] || announcer.events[1] != want[1] {
		t.Fatalf("events = %v, want %v", announcer.events, want)
	}
}

func TestSaveAnnouncesNothingTheStoreRefused(t *testing.T) {
	refused := errors.New("db down")
	announcer := &recordingAnnouncer{}
	err := (Writer{Messages: &recordingStore{fail: refused}, Announcer: announcer}).Save(&conversation.Message{ID: "m1", EntryID: "e1"})
	if !errors.Is(err, refused) {
		t.Fatalf("err = %v", err)
	}
	if len(announcer.events) != 0 {
		t.Fatalf("a change that was not stored must not reach any screen: %v", announcer.events)
	}
}

func TestSaveWithoutAStoreFailsClosed(t *testing.T) {
	if err := (Writer{}).Save(&conversation.Message{ID: "m1"}); !errors.Is(err, ErrNoStore) {
		t.Fatalf("err = %v, want ErrNoStore", err)
	}
}

func TestStampKeepsTheProviderTimeAndFallsBackToNowWhenItIsMissing(t *testing.T) {
	sent := time.Date(2026, 10, 18, 12, 0, 0, 0, time.FixedZone("BRT", -3*3600))
	if got := Stamp(sent); !got.Equal(sent) || got.Location() != time.UTC {
		t.Fatalf("Stamp(provider time) = %v, want the same instant in UTC", got)
	}
	before := time.Now().UTC()
	if got := Stamp(time.Time{}); got.Before(before) || got.After(time.Now().UTC()) {
		t.Fatalf("Stamp(zero) = %v, want now", got)
	}
}
