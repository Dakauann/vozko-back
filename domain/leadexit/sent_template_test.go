package leadexit

import (
	"encoding/json"
	"testing"
	"time"

	"vozko/domain/buttonaction"
	"vozko/domain/conversation"
)

func TestATappedMessageMustHaveBeenSentToThisNumberInThisWorkspace(t *testing.T) {
	sent := SentTemplate{MessageID: "wamid.1", WorkspaceID: "ws-1", RecipientNumber: "5511987654321", Template: promoRef}
	cases := []struct {
		name      string
		workspace string
		number    string
		want      bool
	}{
		{"same workspace and number", "ws-1", "5511987654321", true},
		{"same number without the ninth digit", "ws-1", "551187654321", true},
		{"another workspace", "ws-2", "5511987654321", false},
		{"another number", "ws-1", "5511912345678", false},
		{"no workspace", "", "5511987654321", false},
		{"no number", "ws-1", "", false},
	}
	for _, tc := range cases {
		if got := sent.SentTo(tc.workspace, tc.number); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
	if (SentTemplate{WorkspaceID: "ws-1"}).SentTo("ws-1", "5511987654321") {
		t.Fatal("a message without a recipient matched")
	}
}

func templateMessage(name, language string, at time.Time) *conversation.Message {
	meta, _ := json.Marshal(map[string]any{"template_name": name, "language": language, "body_text": "Oferta"})
	wamid := "wamid." + name
	return &conversation.Message{
		ID: "m-" + name, MessageType: conversation.MessageTypeTemplate, SentBy: conversation.SentByCampaign("camp-1"),
		To: "5511987654321", Metadata: meta, WhatsAppMessageID: &wamid, CreatedAt: at,
	}
}

func TestTheTemplateOfAStoredMessageComesFromItsMetadata(t *testing.T) {
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	sent, ok := TemplateOfMessage(templateMessage("Promo_Outubro", "pt_BR", at))
	if !ok || sent.Template != promoRef || sent.CampaignID != "camp-1" || sent.RecipientNumber != "5511987654321" || sent.MessageID != "wamid.Promo_Outubro" {
		t.Fatalf("got %+v, %v", sent, ok)
	}
	text := &conversation.Message{MessageType: conversation.MessageTypeAIResponse, Text: "oi"}
	if _, ok := TemplateOfMessage(text); ok {
		t.Fatal("a text message has no template")
	}
	noLanguage := templateMessage("promo", "", at)
	if _, ok := TemplateOfMessage(noLanguage); ok {
		t.Fatal("a template without its language cannot be resolved")
	}
	if _, ok := TemplateOfMessage(nil); ok {
		t.Fatal("nil message")
	}
}

func TestTheLastTemplateTheLeadReceivedIsTheNewestTemplateMessage(t *testing.T) {
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	reply := &conversation.Message{MessageType: conversation.MessageTypeUserMessage, SentBy: conversation.SentByContact("5511987654321"), Text: "oi", CreatedAt: at.Add(3 * time.Minute)}
	history := []*conversation.Message{
		templateMessage("boleto", "pt_BR", at),
		templateMessage("promo_outubro", "pt_BR", at.Add(time.Minute)),
		reply,
	}
	last, ok := LastTemplateIn(history)
	if !ok || last.Template != promoRef {
		t.Fatalf("got %+v, %v", last, ok)
	}
	if _, ok := LastTemplateIn([]*conversation.Message{reply, nil}); ok {
		t.Fatal("a conversation without templates has no last template")
	}
}

func TestTemplateWordsListTheRepliedTemplateFirst(t *testing.T) {
	boleto := buttonaction.TemplateRef{Name: "boleto", Language: "pt_BR"}
	words := TemplatesInOrder(&SentTemplate{Template: boleto}, &SentTemplate{Template: promoRef})
	if len(words) != 2 || words[0].Template != boleto || words[1].Template != promoRef {
		t.Fatalf("got %+v", words)
	}
	if got := TemplatesInOrder(&SentTemplate{Template: promoRef}, &SentTemplate{Template: promoRef}); len(got) != 1 {
		t.Fatalf("repeats: %+v", got)
	}
	if got := TemplatesInOrder(&SentTemplate{TemplateID: "tmpl-1"}, &SentTemplate{TemplateID: "tmpl-1", Template: promoRef}); len(got) != 1 {
		t.Fatalf("same template id: %+v", got)
	}
	if got := TemplatesInOrder(&SentTemplate{}, nil); len(got) != 0 {
		t.Fatalf("a sent message without a template: %+v", got)
	}
	if got := TemplatesInOrder(nil, nil); len(got) != 0 {
		t.Fatalf("none: %+v", got)
	}
}
