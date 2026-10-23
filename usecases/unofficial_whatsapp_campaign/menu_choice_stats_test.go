package unofficial_whatsapp_campaign

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"vozko/domain/campaign"
	"vozko/domain/choicestats"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

type spyChoices struct {
	mu    sync.Mutex
	sends []choicestats.Send
}

func (s *spyChoices) RecordSend(_ context.Context, send choicestats.Send) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sends = append(s.sends, send)
}

func (s *spyChoices) RecordClick(context.Context, choicestats.Click) {}

func TestAMenuCampaignSendIsRecordedWithTheRowsWhatsAppShows(t *testing.T) {
	h := newHarness(t)
	spy := &spyChoices{}
	h.consumer.deps.Choices = spy
	campID, entryID := h.seed(t)
	options := make([]uw.InteractiveOption, 0, uw.MaxListOptions+2)
	for i := 0; i < uw.MaxListOptions+2; i++ {
		options = append(options, uw.InteractiveOption{ID: "op" + string(rune('a'+i)), Title: "Opção"})
	}
	camp, _ := h.campaigns.FindByID(campID)
	menu := *camp
	menu.Message = uwc.MessageSpec{Kind: uwc.KindMenu, Bodies: []string{"Escolha"}, Style: uw.InteractiveStyleList, Options: options}
	h.campaigns.put(&menu)

	h.run(campID, entryID)

	if len(spy.sends) != 1 {
		t.Fatalf("sends = %+v", spy.sends)
	}
	send := spy.sends[0]
	if send.PromptKey != choicestats.UnofficialCampaignPromptKey(entryID) || send.WorkspaceID != "ws-1" || send.ProviderMessageID != "prov-"+h.entries.get(entryID).ConversationID+"-1" {
		t.Fatalf("send = %+v", send)
	}
	if send.Source != (choicestats.Source{Kind: choicestats.SourceUnofficialCampaign, ID: campID}) {
		t.Fatalf("source = %+v", send.Source)
	}
	want := make([]string, 0, uw.MaxListOptions)
	for _, o := range options[:uw.MaxListOptions] {
		want = append(want, o.ID)
	}
	if !reflect.DeepEqual(send.Shown, want) {
		t.Fatalf("shown = %v", send.Shown)
	}
}

func TestATextCampaignSendIsNotAChoice(t *testing.T) {
	h := newHarness(t)
	spy := &spyChoices{}
	h.consumer.deps.Choices = spy
	campID, entryID := h.seed(t)
	h.run(campID, entryID)
	if h.entries.get(entryID).Status != campaign.SendStatusSent || len(spy.sends) != 0 {
		t.Fatalf("sends = %+v", spy.sends)
	}
}

func TestTheConsumerRefusesToStartWithoutTheChoiceRecorder(t *testing.T) {
	h := newHarness(t)
	h.consumer.deps.Choices = nil
	if err := h.consumer.Start(); err == nil {
		t.Fatal("the consumer started without the recorder")
	}
}
