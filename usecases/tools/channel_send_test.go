package tools_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/channel"
	"vozko/domain/conversation"
	"vozko/domain/media"
	"vozko/domain/shared"
)

// Adopting the shared agent turn made these tools reachable from Telegram and
// Instagram. Reachable and WhatsApp-bound is the worst combination: the model is
// offered a tool it will confidently call, and the customer gets nothing.
//
// These pin that the adapter path is taken for other channels, that WhatsApp is
// untouched, and that a channel which cannot present choices says so rather than
// sending a question with nothing to tap.

type toolAdapter struct {
	entryType   shared.EntryType
	windowOpen  bool
	windowErr   error
	sentMedia   *conversation.SendMediaRequest
	resolveFail bool
}

func (a *toolAdapter) EntryType() shared.EntryType { return a.entryType }
func (a *toolAdapter) ResolveEntry(_ context.Context, id string) (*conversation.EntryContext, error) {
	if a.resolveFail {
		return nil, errors.New("no such entry")
	}
	return &conversation.EntryContext{EntryID: id, EntryType: a.entryType, ContactRef: "c1"}, nil
}
func (a *toolAdapter) WindowState(context.Context, *conversation.EntryContext) (conversation.WindowState, error) {
	if a.windowOpen {
		return conversation.OpenWindow(nil), a.windowErr
	}
	return conversation.ClosedWindow(conversation.WindowReasonExpired), a.windowErr
}
func (a *toolAdapter) SendText(context.Context, *conversation.EntryContext, conversation.SendTextRequest) (*conversation.SendOutcome, error) {
	return &conversation.SendOutcome{ProviderMessageID: "t1"}, nil
}
func (a *toolAdapter) SendMedia(_ context.Context, _ *conversation.EntryContext, req conversation.SendMediaRequest) (*conversation.SendOutcome, error) {
	a.sentMedia = &req
	return &conversation.SendOutcome{ProviderMessageID: "m1"}, nil
}

type interactiveToolAdapter struct {
	toolAdapter
	sentOptions *conversation.SendInteractiveRequest
}

func (a *interactiveToolAdapter) SendInteractive(_ context.Context, _ *conversation.EntryContext, req conversation.SendInteractiveRequest) (*conversation.SendOutcome, error) {
	a.sentOptions = &req
	return &conversation.SendOutcome{ProviderMessageID: "i1"}, nil
}
func (a *interactiveToolAdapter) InteractiveLimits() channel.InteractiveLimits {
	return channel.InteractiveLimits{MaxOptionsButtons: 100, MaxOptionsList: 100}
}

func telegramSeeds() map[string]interface{} {
	return map[string]interface{}{
		"__entry_id":   "conv-1",
		"__entry_type": string(shared.EntryTypeTelegram),
	}
}

func TestToolAdapterIsUsedForNonWhatsAppChannels(t *testing.T) {
	adapter := &toolAdapter{entryType: shared.EntryTypeTelegram, windowOpen: true}
	reg := conversation.NewAdapterRegistry(adapter)

	_, ec, ok := resolveToolAdapter(context.Background(), reg, telegramSeeds())
	if !ok {
		t.Fatal("Telegram must resolve to its adapter")
	}
	if ec.EntryID != "conv-1" {
		t.Errorf("entry = %q", ec.EntryID)
	}
}

// WhatsApp keeps its dedicated path: business phone resolution, image
// normalisation, link fallback and the lead window do not generalise.
func TestWhatsAppNeverTakesTheAdapterPath(t *testing.T) {
	adapter := &toolAdapter{entryType: shared.EntryTypeWhatsApp, windowOpen: true}
	reg := conversation.NewAdapterRegistry(adapter)

	config := map[string]interface{}{
		"__entry_id":   "conv-1",
		"__entry_type": string(shared.EntryTypeWhatsApp),
	}
	if _, _, ok := resolveToolAdapter(context.Background(), reg, config); ok {
		t.Error("WhatsApp must stay on its own path")
	}
}

// Without seeds there is no conversation to address. Falling through to the
// WhatsApp path lets it fail with its own honest message.
func TestNoSeedsFallsThroughToWhatsApp(t *testing.T) {
	reg := conversation.NewAdapterRegistry(&toolAdapter{entryType: shared.EntryTypeTelegram, windowOpen: true})

	for _, config := range []map[string]interface{}{
		nil,
		{"__entry_id": "conv-1"},
		{"__entry_type": "telegram"},
	} {
		if _, _, ok := resolveToolAdapter(context.Background(), reg, config); ok {
			t.Errorf("config %v must not resolve an adapter", config)
		}
	}
}

func TestNoRegistryFallsThroughToWhatsApp(t *testing.T) {
	if _, _, ok := resolveToolAdapter(context.Background(), nil, telegramSeeds()); ok {
		t.Error("a nil registry must not resolve")
	}
}

func TestMediaIsSentThroughTheAdapter(t *testing.T) {
	adapter := &toolAdapter{entryType: shared.EntryTypeTelegram, windowOpen: true}
	ec := &conversation.EntryContext{EntryID: "conv-1", EntryType: shared.EntryTypeTelegram}

	res, err := sendMediaViaAdapter(context.Background(), adapter, ec,
		&media.Media{URL: "https://cdn.example/tabela.pdf", Type: media.MediaType("document")}, "tabela de preços")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if adapter.sentMedia == nil {
		t.Fatal("nothing was sent")
	}
	if adapter.sentMedia.Caption != "tabela de preços" {
		t.Errorf("caption = %q", adapter.sentMedia.Caption)
	}
	if adapter.sentMedia.FileName != "tabela.pdf" {
		t.Errorf("FileName = %q, want it derived from the URL", adapter.sentMedia.FileName)
	}
	if m, _ := res.Result.(map[string]interface{}); m["message_id"] != "m1" {
		t.Errorf("result = %+v", res.Result)
	}
}

// A closed window is a normal state, not a fault: the run must continue so the
// agent can explain, rather than erroring the whole turn.
func TestAClosedWindowIsReportedNotThrown(t *testing.T) {
	adapter := &toolAdapter{entryType: shared.EntryTypeInstagram, windowOpen: false}
	ec := &conversation.EntryContext{EntryID: "conv-1", EntryType: shared.EntryTypeInstagram}

	res, err := sendMediaViaAdapter(context.Background(), adapter, ec, &media.Media{URL: "x.png", Type: media.MediaType("image")}, "")
	if err != nil {
		t.Fatalf("a closed window must not be an error: %v", err)
	}
	if !res.IsError {
		t.Error("the model must be told the send did not happen")
	}
	if adapter.sentMedia != nil {
		t.Error("nothing should have been sent")
	}
}

func TestOptionsAreSentThroughTheInteractiveCapability(t *testing.T) {
	adapter := &interactiveToolAdapter{toolAdapter: toolAdapter{entryType: shared.EntryTypeTelegram, windowOpen: true}}
	ec := &conversation.EntryContext{EntryID: "conv-1", EntryType: shared.EntryTypeTelegram}

	if _, err := sendOptionsViaAdapter(context.Background(), adapter, ec, conversation.SendInteractiveRequest{
		Body:    "Escolha:",
		Options: []conversation.InteractiveOption{{ID: "sim", Title: "Sim"}},
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if adapter.sentOptions == nil || len(adapter.sentOptions.Options) != 1 {
		t.Fatalf("options = %+v", adapter.sentOptions)
	}
}

// A channel with a send path but no interactive capability must refuse clearly.
// Sending the question without its options leaves the contact nothing to tap.
func TestAChannelWithoutChoicesRefusesInsteadOfSendingAQuestion(t *testing.T) {
	adapter := &toolAdapter{entryType: shared.EntryTypeTelegram, windowOpen: true}
	ec := &conversation.EntryContext{EntryID: "conv-1", EntryType: shared.EntryTypeTelegram}

	res, err := sendOptionsViaAdapter(context.Background(), adapter, ec, conversation.SendInteractiveRequest{
		Body:    "Escolha:",
		Options: []conversation.InteractiveOption{{ID: "sim", Title: "Sim"}},
	})
	if err != nil {
		t.Fatalf("an unsupported capability must not be an error: %v", err)
	}
	if !res.IsError {
		t.Error("the model must learn the options were not shown")
	}
}

func TestAdapterMediaKindMapsOntoTheSharedVocabulary(t *testing.T) {
	for _, tc := range []struct {
		stored media.MediaType
		want   string
	}{
		{"image", "image"},
		{"video", "video"},
		{"audio", "audio"},
		{"document", "document"},
		// A sticker has no cross-channel equivalent; document is the honest
		// fallback every adapter accepts.
		{"sticker", "document"},
	} {
		if got := adapterMediaKind(&media.Media{Type: tc.stored}); got != tc.want {
			t.Errorf("kind(%q) = %q, want %q", tc.stored, got, tc.want)
		}
	}
}
