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
	return conversation.ShowingOptions(&conversation.SendOutcome{ProviderMessageID: "i1"}, req.Composed().ShowingAs(channel.InteractiveStyleButtons, req.Options[:1])), nil
}
func (a *interactiveToolAdapter) InteractiveLimits() channel.InteractiveLimits {
	return channel.InteractiveLimits{MaxOptionsButtons: 100, MaxOptionsList: 100}
}

func telegramSeeds() map[string]interface{} {
	return map[string]interface{}{
		"__entry_id":     "conv-1",
		"__entry_type":   string(shared.EntryTypeTelegram),
		"__workspace_id": "ws-1",
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

	var recorded []conversation.MessageHistoryRecord
	if _, err := sendOptionsViaAdapter(context.Background(), adapter, ec, conversation.SendInteractiveRequest{
		Body:    "Escolha:",
		Options: []conversation.InteractiveOption{{ID: "sim", Title: "Sim"}, {ID: "nao", Title: "Não"}},
	}, func(r conversation.MessageHistoryRecord) { recorded = append(recorded, r) }); err != nil {
		t.Fatalf("send: %v", err)
	}
	if adapter.sentOptions == nil || len(adapter.sentOptions.Options) != 2 {
		t.Fatalf("options = %+v", adapter.sentOptions)
	}
	if len(recorded) != 1 || recorded[0].ProviderMessageID != "i1" || recorded[0].Text != "Escolha:" {
		t.Fatalf("recorded = %+v", recorded)
	}
	shown := recorded[0].Interactive
	if shown == nil || shown.Kind != conversation.InteractiveKindButtons || len(shown.Buttons) != 1 || shown.Buttons[0].ID != "sim" {
		t.Fatalf("the transcript must keep only what the channel showed, got %+v", shown)
	}
}

func TestOfficialOptionsAreRecordedOnlyForTheConversationContact(t *testing.T) {
	input := conversation.SendButtonMessageInput{To: "5511999990000", BodyText: "Escolha", Buttons: []conversation.InteractiveButton{{Type: conversation.ButtonTypeReply, ID: "sim", Title: "Sim"}}}
	config := map[string]interface{}{"__entry_id": "entry-1", "__recipient_phone": "5511999990000", "__business_phone_id": "bp-1"}

	record := officialOptionsRecord(config, input, "wamid.1")
	if record.EntryID != "entry-1" || record.MessageID != "wamid.1" || record.Interactive == nil || record.Interactive.Buttons[0].Title != "Sim" {
		t.Fatalf("record = %+v", record)
	}

	config["__recipient_phone"] = "5511888880000"
	if got := officialOptionsRecord(config, input, "wamid.2"); got.EntryID != "" {
		t.Fatalf("a send to another number must not land on this conversation, got %+v", got)
	}
}

func TestTheOptionsToolRecordsTheSendAsTheAgent(t *testing.T) {
	history := &capturingHistory{}
	tool := &sendWhatsappButtonMessageTool{history: history}
	tool.recordOptions(context.Background(), map[string]interface{}{"__agent_id": "agent-1"}, conversation.MessageHistoryRecord{EntryID: "e1", EntryType: shared.EntryTypeTelegram, Text: "Escolha"})

	if len(history.records) != 1 {
		t.Fatalf("records = %+v", history.records)
	}
	got := history.records[0]
	if got.SentBy != conversation.SentByAI("agent-1") || got.MessageType != conversation.MessageTypeAIResponse || got.Channel != conversation.MessageChannel(shared.EntryTypeTelegram) {
		t.Fatalf("record = %+v", got)
	}

	tool.recordOptions(context.Background(), nil, conversation.MessageHistoryRecord{EntryType: shared.EntryTypeTelegram, Text: "Escolha"})
	if len(history.records) != 1 {
		t.Fatal("a send outside a conversation has nowhere to be recorded")
	}
}

type capturingHistory struct {
	records []conversation.MessageHistoryRecord
}

func (h *capturingHistory) Record(_ context.Context, r conversation.MessageHistoryRecord) error {
	h.records = append(h.records, r)
	return nil
}

func TestAChannelWithoutChoicesRefusesInsteadOfSendingAQuestion(t *testing.T) {
	adapter := &toolAdapter{entryType: shared.EntryTypeTelegram, windowOpen: true}
	ec := &conversation.EntryContext{EntryID: "conv-1", EntryType: shared.EntryTypeTelegram}

	res, err := sendOptionsViaAdapter(context.Background(), adapter, ec, conversation.SendInteractiveRequest{
		Body:    "Escolha:",
		Options: []conversation.InteractiveOption{{ID: "sim", Title: "Sim"}},
	}, func(conversation.MessageHistoryRecord) { t.Fatal("nothing was sent, so nothing is recorded") })
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
		{"sticker", "document"},
	} {
		if got := adapterMediaKind(&media.Media{Type: tc.stored}); got != tc.want {
			t.Errorf("kind(%q) = %q, want %q", tc.stored, got, tc.want)
		}
	}
}
