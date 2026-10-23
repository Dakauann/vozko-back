package template_usecase

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"vozko/domain/choicestats"
	"vozko/domain/conversation"
	"vozko/domain/whatsapp/template"
	"vozko/domain/whatsappform"
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

func withButtons(spy *spyChoices) func(*BilledTemplateSenderDeps) {
	return func(d *BilledTemplateSenderDeps) {
		tmpl := approvedUtility()
		tmpl.Components = []template.TemplateComponent{
			{Type: "BODY", Text: "Seu horário"},
			{Type: "BUTTONS", Buttons: []template.TemplateButton{
				{Type: "QUICK_REPLY", Text: "Confirmo"},
				{Type: "URL", Text: "Mapa", URL: choicestats.TrackedButtonURL(testLinkBase, "Ab3x")},
			}},
		}
		d.Templates = &stubTemplateRepo{tmpl: tmpl}
		d.Choices = spy
		d.ClickLinkBase = testLinkBase
		d.ClickTokens = testClickTokens
	}
}

func TestAnAcceptedTemplateIsRecordedUnderItsAttemptWithTheTrackedLinkToken(t *testing.T) {
	spy := &spyChoices{}
	h := newHarness(t, withButtons(spy))
	in := validInput()
	in.Origin = whatsappform.OriginInbox
	result, err := h.uc.Execute(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(spy.sends) != 1 {
		t.Fatalf("sends = %+v", spy.sends)
	}
	send := spy.sends[0]
	key := choicestats.TemplateAttemptPromptKey(result.AttemptID)
	if send.PromptKey != key || send.ProviderMessageID != "wamid.1" || send.WorkspaceID != "ws-1" {
		t.Fatalf("send = %+v", send)
	}
	if send.Source != (choicestats.Source{Kind: choicestats.SourceInbox, TemplateID: "tpl-1"}) {
		t.Fatalf("source = %+v", send.Source)
	}
	if !reflect.DeepEqual(send.Shown, []string{"template_button:0", "template_url:1"}) {
		t.Fatalf("shown = %v", send.Shown)
	}
	var token string
	for _, b := range h.client.lastIn.Buttons {
		if b.SubType == conversation.TemplateButtonSubTypeURL {
			token = b.Text
		}
	}
	if index, signed, ok := testClickTokens.Verify(token); !ok || index != 1 || signed != key {
		t.Fatalf("url suffix = %q, want the click token of this attempt", token)
	}

	if _, err := h.uc.Execute(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if len(spy.sends) != 1 {
		t.Fatal("a replayed send was recorded twice")
	}
}

func TestARefusedTemplateIsNeverRecorded(t *testing.T) {
	spy := &spyChoices{}
	h := newHarness(t, withButtons(spy))
	h.client.err = errors.New("meta refused")
	h.client.out = &conversation.SendTextMessageOutput{ResponseStatus: 400}
	_, _ = h.uc.Execute(context.Background(), validInput())
	if len(spy.sends) != 0 {
		t.Fatalf("sends = %+v", spy.sends)
	}
}

func TestTheBilledSenderNeedsItsRecorder(t *testing.T) {
	_, err := NewBilledTemplateSendUseCase(BilledTemplateSenderDeps{
		Templates: &stubTemplateRepo{tmpl: approvedUtility()}, Attempts: newFakeAttempts(), ClientFactory: &stubFactory{},
		Consume: &countingBilling{}, Ledger: &fakeLedger{refs: map[string]bool{}}, Inflight: &fakeInflight{}, BalanceChecker: &fakeBalanceChecker{},
	})
	if !errors.Is(err, choicestats.ErrRecorderMissing) {
		t.Fatalf("err = %v", err)
	}
}

func TestTheBilledSenderNeedsItsClickTokens(t *testing.T) {
	_, err := NewBilledTemplateSendUseCase(BilledTemplateSenderDeps{
		Templates: &stubTemplateRepo{tmpl: approvedUtility()}, Attempts: newFakeAttempts(), ClientFactory: &stubFactory{},
		Consume: &countingBilling{}, Ledger: &fakeLedger{refs: map[string]bool{}}, Inflight: &fakeInflight{}, BalanceChecker: &fakeBalanceChecker{},
		Choices: &spyChoices{},
	})
	if !errors.Is(err, choicestats.ErrClickTokenSecretMissing) {
		t.Fatalf("err = %v", err)
	}
}
