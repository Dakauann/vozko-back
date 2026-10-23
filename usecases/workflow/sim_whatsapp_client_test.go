package workflow_usecase

import (
	"context"
	"testing"

	"vozko/domain/conversation"
)

func TestSimWhatsAppClientRefusesMoreButtonsThanWhatsAppAllows(t *testing.T) {
	outCh := make(chan SimOutboundMessage, 1)
	effects := newSimEffects()
	client := newSimWhatsAppClient(newSimChannel(sendInto(outCh), effects))
	buttons := make([]conversation.InteractiveButton, 0, conversation.WhatsAppButtonMaxCount+1)
	for _, id := range []string{"a", "b", "c", "d"} {
		buttons = append(buttons, conversation.InteractiveButton{ID: id, Title: id})
	}

	_, err := client.SendButtonMessage(context.Background(), conversation.SendButtonMessageInput{
		To:       "5511999990000",
		BodyText: "MENU PRINCIPAL",
		Buttons:  buttons,
	})

	if err == nil {
		t.Fatal("err = nil, want the WhatsApp button limit to be enforced")
	}
	effects.drain()
	if len(outCh) != 0 {
		t.Fatal("a refused message must not reach the simulated chat")
	}
}

func TestSimWhatsAppClientRefusesAListWithoutRows(t *testing.T) {
	outCh := make(chan SimOutboundMessage, 1)
	effects := newSimEffects()
	client := newSimWhatsAppClient(newSimChannel(sendInto(outCh), effects))

	_, err := client.SendListMessage(context.Background(), conversation.SendListMessageInput{
		To:         "5511999990000",
		BodyText:   "Escolha",
		ButtonText: "Escolha",
	})

	if err == nil {
		t.Fatal("err = nil, want an empty list to be refused")
	}
	effects.drain()
	if len(outCh) != 0 {
		t.Fatal("a refused message must not reach the simulated chat")
	}
}

func TestSimWhatsAppClientDeliversAValidButtonMessage(t *testing.T) {
	outCh := make(chan SimOutboundMessage, 1)
	effects := newSimEffects()
	client := newSimWhatsAppClient(newSimChannel(sendInto(outCh), effects))

	_, err := client.SendButtonMessage(context.Background(), conversation.SendButtonMessageInput{
		To:       "5511999990000",
		BodyText: "Deseja continuar?",
		Buttons:  []conversation.InteractiveButton{{ID: "sim", Title: "Sim"}},
	})

	if err != nil {
		t.Fatal(err)
	}
	effects.drain()
	if len(outCh) != 1 {
		t.Fatal("a valid message must reach the simulated chat")
	}
}
