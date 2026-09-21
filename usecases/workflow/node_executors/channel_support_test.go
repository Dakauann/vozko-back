package node_executors

import (
	"context"
	"testing"
	"time"

	"vozko/domain/channel"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type supportAdapter struct{ entryType shared.EntryType }

func (a supportAdapter) EntryType() shared.EntryType { return a.entryType }
func (a supportAdapter) ResolveEntry(context.Context, string) (*conversation.EntryContext, error) {
	return &conversation.EntryContext{EntryID: "e1"}, nil
}
func (a supportAdapter) WindowState(context.Context, *conversation.EntryContext) (conversation.WindowState, error) {
	return conversation.OpenWindow(nil), nil
}
func (a supportAdapter) SendText(context.Context, *conversation.EntryContext, conversation.SendTextRequest) (*conversation.SendOutcome, error) {
	return &conversation.SendOutcome{}, nil
}
func (a supportAdapter) SendMedia(context.Context, *conversation.EntryContext, conversation.SendMediaRequest) (*conversation.SendOutcome, error) {
	return &conversation.SendOutcome{}, nil
}

type interactiveSupportAdapter struct{ supportAdapter }

func (a interactiveSupportAdapter) SendInteractive(context.Context, *conversation.EntryContext, conversation.SendInteractiveRequest) (*conversation.SendOutcome, error) {
	return &conversation.SendOutcome{ProviderMessageID: "m1"}, nil
}
func (a interactiveSupportAdapter) InteractiveLimits() channel.InteractiveLimits {
	return channel.InteractiveLimits{MaxOptionsButtons: 100, MaxOptionsList: 100, MaxPayloadBytes: 64}
}

func telegramRun() *workflow.WorkflowRun {
	return &workflow.WorkflowRun{ID: "r1", EntryID: "e1", EntryType: string(shared.EntryTypeTelegram)}
}

func TestInteractiveIsUnsupportedWithoutAnAdapterRegistry(t *testing.T) {
	s := newChannelSender(SenderDeps{})

	if s.SupportsInteractive(telegramRun()) {
		t.Error("no registry must not report support")
	}
	if s.Supports(telegramRun()) {
		t.Error("no registry must not report a send path either")
	}
}

func TestInteractiveIsSupportedOnceTheRegistryIsWired(t *testing.T) {
	s := newChannelSender(SenderDeps{
		Adapters: conversation.NewAdapterRegistry(
			interactiveSupportAdapter{supportAdapter{entryType: shared.EntryTypeTelegram}},
		),
	})

	if !s.SupportsInteractive(telegramRun()) {
		t.Error("Telegram must present choices once its adapter is registered")
	}
	if !s.Supports(telegramRun()) {
		t.Error("Telegram must report a send path")
	}
}

func TestASenderHoldingTheLiveRegistrySeesLaterChannels(t *testing.T) {
	live := conversation.NewLiveAdapterRegistry()
	s := newChannelSender(SenderDeps{Adapters: live})

	if s.SupportsInteractive(telegramRun()) {
		t.Fatal("nothing is registered yet")
	}

	live.Replace(interactiveSupportAdapter{supportAdapter{entryType: shared.EntryTypeTelegram}})

	if !s.SupportsInteractive(telegramRun()) {
		t.Error("a channel registered after the sender was built must still be supported")
	}
}

func TestAChannelWithoutTheCapabilityIsNotInteractive(t *testing.T) {
	s := newChannelSender(SenderDeps{
		Adapters: conversation.NewAdapterRegistry(
			supportAdapter{entryType: shared.EntryTypeTelegram},
		),
	})

	if s.SupportsInteractive(telegramRun()) {
		t.Error("a plain adapter must not claim the interactive capability")
	}
	if !s.Supports(telegramRun()) {
		t.Error("it can still send text and media")
	}
}

func TestInteractiveSupportReportsLimitsFromTheRegistry(t *testing.T) {
	s := newChannelSender(SenderDeps{
		Adapters: conversation.NewAdapterRegistry(
			interactiveSupportAdapter{supportAdapter{entryType: shared.EntryTypeTelegram}},
		),
	})

	support := s.InteractiveSupport()
	limits, ok := support[shared.EntryTypeTelegram]
	if !ok {
		t.Fatalf("support = %+v, want Telegram", support)
	}
	if limits.MaxPayloadBytes != 64 {
		t.Errorf("MaxPayloadBytes = %d, want the adapter's own number", limits.MaxPayloadBytes)
	}
}

type presenceSupportAdapter struct {
	supportAdapter
	typingCalls int
	sent        []string
}

func (a *presenceSupportAdapter) SendTyping(context.Context, *conversation.EntryContext, bool) error {
	a.typingCalls++
	return nil
}
func (a *presenceSupportAdapter) MarkSeen(context.Context, *conversation.EntryContext, string) error {
	return nil
}
func (a *presenceSupportAdapter) SendText(_ context.Context, _ *conversation.EntryContext, req conversation.SendTextRequest) (*conversation.SendOutcome, error) {
	a.sent = append(a.sent, req.Body)
	return &conversation.SendOutcome{ProviderMessageID: "m"}, nil
}

func TestSegmentsAreDeliveredOnAnAdapterBackedChannel(t *testing.T) {
	adapter := &presenceSupportAdapter{supportAdapter: supportAdapter{entryType: shared.EntryTypeTelegram}}
	s := newChannelSender(SenderDeps{Adapters: conversation.NewAdapterRegistry(adapter)})

	delivered, err := s.SendSegments(context.Background(), telegramRun(),
		[]string{"Olá", "Como posso ajudar?"}, func(time.Duration) {})
	if err != nil {
		t.Fatalf("SendSegments: %v", err)
	}
	if !delivered {
		t.Fatal("every segment should have been delivered")
	}
	if len(adapter.sent) != 2 || adapter.sent[0] != "Olá" {
		t.Errorf("sent = %v, want both segments in order", adapter.sent)
	}
	if adapter.typingCalls != 2 {
		t.Errorf("typing calls = %d, want one per segment", adapter.typingCalls)
	}
}

func TestSegmentsDeliverWithoutAPresenceCapability(t *testing.T) {
	s := newChannelSender(SenderDeps{
		Adapters: conversation.NewAdapterRegistry(supportAdapter{entryType: shared.EntryTypeTelegram}),
	})

	delivered, err := s.SendSegments(context.Background(), telegramRun(),
		[]string{"a", "b"}, func(time.Duration) {})
	if err != nil || !delivered {
		t.Errorf("delivered=%v err=%v, want a plain adapter to still send", delivered, err)
	}
}

func TestSegmentsAreWithheldWithoutAnAdapter(t *testing.T) {
	s := newChannelSender(SenderDeps{})
	if delivered, err := s.SendSegments(context.Background(), telegramRun(), []string{"a"}, func(time.Duration) {}); delivered || err != nil {
		t.Errorf("delivered=%v err=%v, want a clean decline", delivered, err)
	}
}
