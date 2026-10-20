package whatsapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/conversation"
)

func TestAReactionIsSentAsACloudAPIReactionMessage(t *testing.T) {
	client, bodies := capturingGraph(t)
	var reactions conversation.WhatsAppReactionClient = client

	if err := reactions.SendReaction(context.Background(), "+55 11 99999-0000", " wamid.target ", "👍"); err != nil {
		t.Fatal(err)
	}
	if err := reactions.SendReaction(context.Background(), "5511999990000", "wamid.target", ""); err != nil {
		t.Fatal(err)
	}
	if len(*bodies) != 2 {
		t.Fatalf("requests = %d, want two", len(*bodies))
	}
	first := (*bodies)[0]
	reaction, _ := first["reaction"].(map[string]any)
	if first["type"] != "reaction" || first["messaging_product"] != "whatsapp" || reaction["message_id"] != "wamid.target" || reaction["emoji"] != "👍" {
		t.Fatalf("body = %v", first)
	}
	removal, _ := (*bodies)[1]["reaction"].(map[string]any)
	if emoji, present := removal["emoji"]; !present || emoji != "" {
		t.Fatalf("a removal must send an empty emoji, body = %v", (*bodies)[1])
	}
}

func TestAReactionTheGraphRefusesIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"outside the window"}}`))
	}))
	t.Cleanup(server.Close)
	client := NewClient(Config{BaseURL: server.URL, PhoneNumberID: "PNID", AccessToken: "tok"}).(*Client)

	if err := client.SendReaction(context.Background(), "5511999990000", "wamid.target", "👍"); err == nil {
		t.Fatal("a refused reaction must surface as an error")
	}
	if err := client.SendReaction(context.Background(), "5511999990000", " ", "👍"); err == nil {
		t.Fatal("a reaction without a target message must be refused before the call")
	}
}
