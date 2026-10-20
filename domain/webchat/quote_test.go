package webchat

import (
	"strings"
	"testing"
	"time"

	"vozko/domain/conversation"
)

func TestQuoteOfShowsTheQuotedLineOnlyWhenTheVisitorCouldSeeIt(t *testing.T) {
	gone := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	visitorLine := &conversation.Message{ID: "m-1", ExternalMessageID: ptr("wc_in_v_abc12345"), MessageType: conversation.MessageTypeUserMessage, SentBy: conversation.SentByContact("v"), Text: "  qual o prazo de entrega?  "}
	cases := []struct {
		name   string
		quoted *conversation.Message
		want   *VisitorQuote
	}{
		{"a visitor message", visitorLine, &VisitorQuote{ID: "wc_in_v_abc12345", Text: "qual o prazo de entrega?"}},
		{"nothing quoted", nil, nil},
		{"an internal tool result never leaks", &conversation.Message{ID: "m-2", MessageType: conversation.MessageTypeToolResult, Text: "segredo"}, nil},
		{"a system line never leaks", &conversation.Message{ID: "m-3", MessageType: conversation.MessageTypeSystem, SentBy: conversation.SentBySystem(), Text: "atribuída"}, nil},
		{"a retracted message shows nothing", &conversation.Message{ID: "m-4", MessageType: conversation.MessageTypeUserMessage, SentBy: conversation.SentByContact("v"), Text: "apagada", RetractedAt: &gone}, nil},
		{"a file without caption shows its name", &conversation.Message{ID: "m-5", MessageType: conversation.MessageTypeMedia, SentBy: conversation.SentByContact("v"), MediaType: conversation.MediaTypeDocument, Media: &conversation.AttachedMedia{URL: "https://cdn.example/f.pdf", Filename: "orcamento.pdf"}}, &VisitorQuote{ID: "m-5", Text: "orcamento.pdf"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := QuoteOf(c.quoted)
			if (got == nil) != (c.want == nil) || got != nil && *got != *c.want {
				t.Fatalf("QuoteOf = %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestAQuoteIsClippedToAShortLine(t *testing.T) {
	long := strings.Repeat("palavra ", 60)
	got := QuoteOf(&conversation.Message{ID: "m-1", MessageType: conversation.MessageTypeOperator, SentBy: conversation.SentByPerson("u"), Text: long})
	if got == nil {
		t.Fatal("quote missing")
	}
	runes := []rune(got.Text)
	if len(runes) > MaxQuoteRunes || !strings.HasSuffix(got.Text, "…") {
		t.Fatalf("quote = %q (%d runes), want at most %d ending with an ellipsis", got.Text, len(runes), MaxQuoteRunes)
	}
}

func TestAQuoteFlattensLineBreaks(t *testing.T) {
	got := QuoteOf(&conversation.Message{ID: "m-1", MessageType: conversation.MessageTypeOperator, SentBy: conversation.SentByPerson("u"), Text: "linha um\n\nlinha  dois"})
	if got == nil || got.Text != "linha um linha dois" {
		t.Fatalf("quote = %+v, want one line", got)
	}
}
