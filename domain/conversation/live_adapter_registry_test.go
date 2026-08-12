package conversation

import (
	"context"
	"testing"

	"vozko/domain/channel"
	"vozko/domain/shared"
)

type fakeAdapter struct{ entryType shared.EntryType }

func (f fakeAdapter) EntryType() shared.EntryType { return f.entryType }
func (f fakeAdapter) ResolveEntry(context.Context, string) (*EntryContext, error) {
	return nil, nil
}
func (f fakeAdapter) WindowState(context.Context, *EntryContext) (WindowState, error) {
	return OpenWindow(nil), nil
}
func (f fakeAdapter) SendText(context.Context, *EntryContext, SendTextRequest) (*SendOutcome, error) {
	return nil, nil
}
func (f fakeAdapter) SendMedia(context.Context, *EntryContext, SendMediaRequest) (*SendOutcome, error) {
	return nil, nil
}

type fakeInteractiveAdapter struct{ fakeAdapter }

func (f fakeInteractiveAdapter) SendInteractive(context.Context, *EntryContext, SendInteractiveRequest) (*SendOutcome, error) {
	return nil, nil
}
func (f fakeInteractiveAdapter) InteractiveLimits() channel.InteractiveLimits {
	return channel.InteractiveLimits{MaxOptionsButtons: 3, MaxOptionsList: 10}
}

// Channel adapters register one at a time during container startup, and several
// consumers are constructed in between. A consumer handed a snapshot sees only
// the channels registered so far, and a missing adapter is indistinguishable
// from "this channel cannot send", so every workflow send node silently skipped
// on Instagram and Telegram while the run reported itself completed.
func TestLiveRegistrySeesAdaptersRegisteredAfterItWasHandedOut(t *testing.T) {
	live := NewLiveAdapterRegistry()

	// A consumer takes the registry now, before any channel has registered.
	var consumer AdapterRegistry = live
	if consumer.Has(shared.EntryTypeTelegram) {
		t.Fatal("an empty registry must not claim to have a channel")
	}

	live.Replace(fakeAdapter{entryType: shared.EntryTypeInstagram})
	live.Replace(
		fakeAdapter{entryType: shared.EntryTypeInstagram},
		fakeAdapter{entryType: shared.EntryTypeTelegram},
	)

	// The consumer's reference must reflect both, without being re-wired.
	if !consumer.Has(shared.EntryTypeTelegram) {
		t.Error("a channel registered after hand-out must still be visible")
	}
	if !consumer.Has(shared.EntryTypeInstagram) {
		t.Error("earlier channels must survive a replace")
	}
	if got := consumer.EntryTypes(); len(got) != 2 {
		t.Errorf("EntryTypes() = %v, want both channels", got)
	}
}

func TestLiveRegistryResolvesTheAdapterItself(t *testing.T) {
	live := NewLiveAdapterRegistry()
	live.Replace(fakeInteractiveAdapter{fakeAdapter{entryType: shared.EntryTypeTelegram}})

	adapter, err := live.For(shared.EntryTypeTelegram)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	// The optional capability must survive the indirection, it is discovered by
	// type assertion, so a wrapper that hid it would disable interactive prompts
	// on every channel at once.
	if _, ok := adapter.(InteractiveAdapter); !ok {
		t.Error("the interactive capability must be reachable through the live registry")
	}
}

func TestLiveRegistryReportsAnUnknownChannelHonestly(t *testing.T) {
	live := NewLiveAdapterRegistry()
	live.Replace(fakeAdapter{entryType: shared.EntryTypeTelegram})

	if _, err := live.For(shared.EntryTypeInstagram); err != ErrNoAdapterForEntryType {
		t.Errorf("err = %v, want ErrNoAdapterForEntryType", err)
	}
}
