package whatsapp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/conversation"
)

func capturingGraph(t *testing.T) (*Client, *[]map[string]any) {
	t.Helper()
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("body: %v", err)
		}
		bodies = append(bodies, body)
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.sent"}]}`))
	}))
	t.Cleanup(server.Close)
	return NewClient(Config{BaseURL: server.URL, PhoneNumberID: "PNID", WABAId: "WABA", AccessToken: "tok"}).(*Client), &bodies
}

func TestAButtonReplyQuotesTheMessageItAnswers(t *testing.T) {
	for name, button := range map[string]conversation.InteractiveButton{
		"reply buttons":     {Type: conversation.ButtonTypeReply, ID: "yes", Title: "Sim"},
		"copy code as text": {Type: conversation.ButtonTypeCopyCode, CopyCode: "ABC123"},
	} {
		t.Run(name, func(t *testing.T) {
			client, bodies := capturingGraph(t)
			_, err := client.SendButtonMessage(context.Background(), conversation.SendButtonMessageInput{
				To: "5511999990000", BodyText: "Escolha", Buttons: []conversation.InteractiveButton{button}, ContextMessageID: " wamid.quoted ",
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(*bodies) != 1 {
				t.Fatalf("requests = %d", len(*bodies))
			}
			quoted, _ := (*bodies)[0]["context"].(map[string]any)
			if quoted["message_id"] != "wamid.quoted" {
				t.Fatalf("context = %v, want the quoted wamid", (*bodies)[0]["context"])
			}
		})
	}
}
