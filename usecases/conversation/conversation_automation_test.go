package conversation_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type recordingBroadcaster struct {
	conversation.EventBroadcaster
	entries chan string
}

func (r *recordingBroadcaster) BroadcastEntryUpdate(entryID, entryType string, _ *conversation.Message) {
	r.entries <- entryType + ":" + entryID
}

func boolPtr(b bool) *bool { return &b }

func TestAutomationIsWrittenThroughTheChannelsOwnSetter(t *testing.T) {
	var gotEntry string
	var gotEnabled *bool

	svc := NewConversationAutomationService(nil)
	svc.Register(shared.EntryTypeTelegram, func(_ context.Context, entryID string, enabled *bool) error {
		gotEntry, gotEnabled = entryID, enabled
		return nil
	})

	if err := svc.SetAutomation(context.Background(), "conv-1", shared.EntryTypeTelegram, boolPtr(false)); err != nil {
		t.Fatalf("SetAutomation: %v", err)
	}
	if gotEntry != "conv-1" {
		t.Errorf("entry = %q", gotEntry)
	}
	if gotEnabled == nil || *gotEnabled != false {
		t.Errorf("enabled = %v, want an explicit false", gotEnabled)
	}
}

func TestClearingTheOverridePassesNilThrough(t *testing.T) {
	var called bool
	var gotEnabled *bool

	svc := NewConversationAutomationService(nil)
	svc.Register(shared.EntryTypeInstagram, func(_ context.Context, _ string, enabled *bool) error {
		called, gotEnabled = true, enabled
		return nil
	})

	if err := svc.SetAutomation(context.Background(), "conv-1", shared.EntryTypeInstagram, nil); err != nil {
		t.Fatalf("SetAutomation: %v", err)
	}
	if !called {
		t.Fatal("the setter must still run when clearing")
	}
	if gotEnabled != nil {
		t.Errorf("enabled = %v, want nil to mean inherit", gotEnabled)
	}
}

func TestAChannelWithNoSetterIsRefusedNotIgnored(t *testing.T) {
	svc := NewConversationAutomationService(nil)
	svc.Register(shared.EntryTypeWhatsApp, func(context.Context, string, *bool) error { return nil })

	err := svc.SetAutomation(context.Background(), "conv-1", shared.EntryTypeTelegram, boolPtr(false))
	if !errors.Is(err, conversation.ErrEntryTypeInvalid) {
		t.Errorf("err = %v, want ErrEntryTypeInvalid, a silent success is what shipped", err)
	}
}

func TestTheBroadcastCarriesTheEntrysOwnChannel(t *testing.T) {
	hub := &recordingBroadcaster{entries: make(chan string, 1)}
	svc := NewConversationAutomationService(hub)
	svc.Register(shared.EntryTypeTelegram, func(context.Context, string, *bool) error {
		return nil
	})

	if err := svc.SetAutomation(context.Background(), "conv-1", shared.EntryTypeTelegram, boolPtr(false)); err != nil {
		t.Fatalf("SetAutomation: %v", err)
	}

	select {
	case got := <-hub.entries:
		if got != "telegram:conv-1" {
			t.Errorf("broadcast = %q, want the entry's own channel", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no broadcast")
	}
}

func TestASetterErrorSurfaces(t *testing.T) {
	sentinel := errors.New("conversation not found")
	svc := NewConversationAutomationService(nil)
	svc.Register(shared.EntryTypeTelegram, func(context.Context, string, *bool) error { return sentinel })

	if err := svc.SetAutomation(context.Background(), "conv-1", shared.EntryTypeTelegram, boolPtr(false)); !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want the setter's own error", err)
	}
}

func TestAMissingEntryIDIsRejected(t *testing.T) {
	svc := NewConversationAutomationService(nil)
	svc.Register(shared.EntryTypeTelegram, func(context.Context, string, *bool) error { return nil })

	if err := svc.SetAutomation(context.Background(), "", shared.EntryTypeTelegram, boolPtr(false)); !errors.Is(err, conversation.ErrConversationNotFound) {
		t.Errorf("err = %v", err)
	}
}
