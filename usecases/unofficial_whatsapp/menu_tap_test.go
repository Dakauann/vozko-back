package unofficial_whatsapp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"vozko/domain/choicestats"
	"vozko/domain/workflow"
)

type spyChoices struct {
	clicks []choicestats.Click
}

func (s *spyChoices) RecordSend(context.Context, choicestats.Send) {}

func (s *spyChoices) RecordClick(_ context.Context, click choicestats.Click) {
	s.clicks = append(s.clicks, click)
}

func (h *autoHarness) deliverTap(t *testing.T, optionID, quoted string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"event": "messages", "instance": "prov-1",
		"data": map[string]any{
			"messageid": "msg-tap", "chatid": "558494409624@s.whatsapp.net", "sender": "558494409624@s.whatsapp.net",
			"sender_pn": "558494409624@s.whatsapp.net", "senderName": "Dakauann", "isGroup": false,
			"messageType": "ButtonsResponseMessage", "text": "Quero saber", "buttonOrListid": optionID, "quoted": quoted,
			"messageTimestamp": time.Now().UnixMilli(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.uc.Execute(context.Background(), &QueuedEvent{InstanceID: "inst-1", Body: body}); err != nil {
		t.Fatalf("delivering the tap: %v", err)
	}
}

func TestAMenuTapCarriesTheSelectionAndCountsOnTheQuotedPrompt(t *testing.T) {
	h := newAutoHarness(t, nil)
	spy := &spyChoices{}
	h.uc.choices = spy

	h.deliverTap(t, "saber", "menu-msg-1")

	events := h.workflows.all()
	if len(events) == 0 {
		t.Fatal("the tap started nothing")
	}
	data := events[0].Data
	if data[workflow.DataKeySelectedOptionID] != "saber" || data[workflow.DataKeySelectedOptionTitle] != "Quero saber" {
		t.Fatalf("data = %+v", data)
	}
	if len(spy.clicks) != 1 {
		t.Fatalf("clicks = %+v", spy.clicks)
	}
	click := spy.clicks[0]
	if click.WorkspaceID != "ws-1" || click.ProviderMessageID != "menu-msg-1" || click.Option != "saber" || click.PromptKey != "" {
		t.Fatalf("click = %+v", click)
	}
}

func TestATapWithoutAQuotedPromptCountsNowhere(t *testing.T) {
	h := newAutoHarness(t, nil)
	spy := &spyChoices{}
	h.uc.choices = spy
	h.deliverTap(t, "saber", "")
	if len(spy.clicks) != 0 {
		t.Fatalf("clicks = %+v", spy.clicks)
	}
}
