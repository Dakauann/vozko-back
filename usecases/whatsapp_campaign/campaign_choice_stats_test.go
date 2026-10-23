package whatsapp_campaign_usecase

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"vozko/domain/choicestats"
	"vozko/domain/conversation"
	"vozko/domain/whatsapp/template"
)

const testLinkBase = "https://vz.co/r"

var testClickTokens, _ = choicestats.NewClickTokens("server-secret")

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

func buttonCampaignTemplate() *template.Template {
	tmpl := approvedMarketingTemplate("tmpl-1")
	tmpl.Components = []template.TemplateComponent{
		{Type: "BODY", Text: "Oferta"},
		{Type: "BUTTONS", Buttons: []template.TemplateButton{
			{Type: "QUICK_REPLY", Text: "Quero"},
			{Type: "URL", Text: "Loja", URL: choicestats.TrackedButtonURL(testLinkBase, "Ab3x")},
		}},
	}
	return tmpl
}

func TestACampaignTemplateSendIsRecordedPerEntryWithItsClickToken(t *testing.T) {
	spy := &spyChoices{}
	rig := newFormConsumerRig("camp-cta", buttonCampaignTemplate(), nil)
	rig.h.consumer.SetChoiceStats(spy, testLinkBase, testClickTokens)
	rig.client.out = &conversation.SendTextMessageOutput{MessageID: "wamid-cta-1"}

	rig.deliver(t, "camp-cta")

	if len(spy.sends) != 1 {
		t.Fatalf("sends = %+v", spy.sends)
	}
	send := spy.sends[0]
	key := choicestats.WhatsAppCampaignPromptKey("entry-1")
	if send.PromptKey != key || send.ProviderMessageID != "wamid-cta-1" || send.WorkspaceID != "ws-1" {
		t.Fatalf("send = %+v", send)
	}
	if send.Source != (choicestats.Source{Kind: choicestats.SourceWhatsAppCampaign, ID: "camp-cta", TemplateID: "tmpl-1"}) {
		t.Fatalf("source = %+v", send.Source)
	}
	if !reflect.DeepEqual(send.Shown, []string{"template_button:0", "template_url:1"}) {
		t.Fatalf("shown = %v", send.Shown)
	}
	var token string
	for _, b := range rig.client.last[0].Buttons {
		if b.SubType == conversation.TemplateButtonSubTypeURL {
			token = b.Text
		}
	}
	if index, signed, ok := testClickTokens.Verify(token); !ok || index != 1 || signed != key {
		t.Fatalf("url suffix = %q", token)
	}
}

func TestTheCampaignConsumerRefusesToStartWithoutTheChoiceRecorder(t *testing.T) {
	h := newTestHarness()
	h.consumer.choices = nil
	if err := h.consumer.Start(); err == nil {
		t.Fatal("started without the recorder")
	}
}

func TestTheCampaignConsumerRefusesToStartWithoutTheClickTokens(t *testing.T) {
	h := newTestHarness()
	h.consumer.clickTokens = nil
	if err := h.consumer.Start(); !errors.Is(err, choicestats.ErrClickTokenSecretMissing) {
		t.Fatalf("err = %v", err)
	}
}
