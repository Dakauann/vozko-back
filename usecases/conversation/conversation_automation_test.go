package conversation_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

// The only way to flip a conversation's automation override was a PATCH on a
// WhatsApp CAMPAIGN entry. Instagram and Telegram conversations have no
// campaign, so the caller resolved no campaign id and returned before issuing a
// request: no error, no feedback, nothing written. The control looked
// functional and did nothing.
//
// These pin that every channel has a setter, and that a channel WITHOUT one is
// refused loudly instead of silently succeeding.

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

// nil is not the same as false: it CLEARS the override so the conversation
// inherits the account switch again. Flattening it to a bool would make
// "inherit" unreachable.
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

// The bug's shape: a channel with no setter. It must be refused, not accepted.
func TestAChannelWithNoSetterIsRefusedNotIgnored(t *testing.T) {
	svc := NewConversationAutomationService(nil)
	svc.Register(shared.EntryTypeWhatsApp, func(context.Context, string, *bool) error { return nil })

	err := svc.SetAutomation(context.Background(), "conv-1", shared.EntryTypeTelegram, boolPtr(false))
	if !errors.Is(err, conversation.ErrEntryTypeInvalid) {
		t.Errorf("err = %v, want ErrEntryTypeInvalid, a silent success is what shipped", err)
	}
}

// The broadcast must carry the entry's OWN type. The handler this replaces
// always broadcast "whatsapp", so a toggle on another channel would not have
// refreshed its own conversation even once the write worked.
func TestTheBroadcastCarriesTheEntrysOwnChannel(t *testing.T) {
	hub := &recordingBroadcaster{entries: make(chan string, 1)}
	svc := NewConversationAutomationService(hub)
	svc.Register(shared.EntryTypeTelegram, func(context.Context, string, *bool) error {
		return nil
	})

	if err := svc.SetAutomation(context.Background(), "conv-1", shared.EntryTypeTelegram, boolPtr(false)); err != nil {
		t.Fatalf("SetAutomation: %v", err)
	}

	// Fired in a goroutine so the write is not held up by it.
	select {
	case got := <-hub.entries:
		if got != "telegram:conv-1" {
			t.Errorf("broadcast = %q, want the entry's own channel", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no broadcast")
	}
}

// A failing write must surface rather than report success and leave the
// operator believing the agent is paused.
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
