package conversation_usecase

import (
	"encoding/json"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/geo"
	"vozko/domain/lead"
	"vozko/domain/shared"
)

func locationPayload(t *testing.T, message string) *conversation.WhatsAppWebhookPayload {
	t.Helper()
	raw := `{"object":"whatsapp_business_account","entry":[{"id":"waba","changes":[{"field":"messages","value":{` +
		`"metadata":{"display_phone_number":"551140001234","phone_number_id":"meta-phone-1"},` +
		`"contacts":[{"wa_id":"5511999990000","profile":{"name":"Maria"}}],"messages":[` + message + `]}}]}]}`
	var payload conversation.WhatsAppWebhookPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	return &payload
}

func TestAnInboundLocationMessageIsKeptWithItsCoordinates(t *testing.T) {
	uc := &handleWhatsAppMessageUseCase{}
	payload := locationPayload(t, `{"from":"5511999990000","id":"wamid.loc","timestamp":"1759900000","type":"location",`+
		`"location":{"latitude":-23.55052,"longitude":-46.633308,"name":"Casa","address":"Rua A, 10"}}`)
	message, _, contact := uc.extractInboundMessage(payload)
	if message == nil {
		t.Fatal("the location message was dropped")
	}
	if contact != "Maria" || message.Location == nil || message.Text == nil {
		t.Fatalf("message = %+v, contact %q", message, contact)
	}
	if want := "📍 Casa, Rua A, 10 (-23.550520, -46.633308)"; message.Text.Body != want {
		t.Fatalf("text = %q, want %q", message.Text.Body, want)
	}

	record := uc.inboundTextRecord(message, inboundTarget{entryID: "entry-1", entryType: shared.EntryTypeWhatsApp, conversationID: "conv-1", businessNumber: "551140001234"},
		&lead.Lead{ID: "lead-1", Number: "5511999990000", Name: "Maria"})
	stored := &conversation.Message{ID: "msg-1", Channel: record.Channel, MessageType: record.MessageType, SentBy: record.SentBy, Text: record.Text, Metadata: record.Metadata}
	candidate, ok := conversation.LocationCandidateOf(stored)
	if !ok || candidate.Point != (geo.Point{Lat: -23.55052, Lng: -46.633308}) || candidate.Channel != conversation.MessageChannelWhatsApp {
		t.Fatalf("stored message %+v is not a location candidate: %+v", record, candidate)
	}
	if record.EntryID != "entry-1" || record.ConversationID != "conv-1" || record.MessageID != "wamid.loc" || record.To != "551140001234" || record.SenderName != "Maria" {
		t.Fatalf("record = %+v", record)
	}
}

func TestATextMessageRecordCarriesNoLocation(t *testing.T) {
	uc := &handleWhatsAppMessageUseCase{}
	payload := locationPayload(t, `{"from":"5511999990000","id":"wamid.txt","timestamp":"1759900000","type":"text","text":{"body":"oi"}}`)
	message, _, _ := uc.extractInboundMessage(payload)
	if message == nil {
		t.Fatal("the text message was dropped")
	}
	record := uc.inboundTextRecord(message, inboundTarget{entryID: "entry-1", entryType: shared.EntryTypeWhatsApp}, nil)
	if record.Metadata != nil || record.Text != "oi" {
		t.Fatalf("record = %+v", record)
	}
}

func TestALocationMessageWithoutCoordinatesIsStillDropped(t *testing.T) {
	uc := &handleWhatsAppMessageUseCase{}
	payload := locationPayload(t, `{"from":"5511999990000","id":"wamid.loc","timestamp":"1759900000","type":"location"}`)
	if message, _, _ := uc.extractInboundMessage(payload); message != nil {
		t.Fatalf("a location message without its location was kept: %+v", message)
	}
}
