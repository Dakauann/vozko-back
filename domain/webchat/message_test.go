package webchat

import (
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/conversation"
)

func ptr(s string) *string { return &s }

func TestProjectMessageShowsOnlyWhatACustomerMaySee(t *testing.T) {
	hidden := []conversation.MessageType{
		conversation.MessageTypeToolCall,
		conversation.MessageTypeToolResult,
		conversation.MessageTypeSystem,
		conversation.MessageTypeTemplate,
		conversation.MessageTypeReaction,
		conversation.MessageTypeUnsupported,
	}
	for _, mt := range hidden {
		m := &conversation.Message{ID: "m1", MessageType: mt, Text: "internal", SentBy: conversation.SentBySystem()}
		if _, ok := ProjectMessage(m); ok {
			t.Errorf("%s must not reach the visitor", mt)
		}
	}
}

func TestProjectMessageNamesTheAuthorWithoutInternalIDs(t *testing.T) {
	at := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	cases := map[string]struct {
		m          conversation.Message
		wantAuthor Author
	}{
		"visitor":  {conversation.Message{MessageType: conversation.MessageTypeUserMessage, SentBy: conversation.SentByContact("v1")}, AuthorVisitor},
		"operator": {conversation.Message{MessageType: conversation.MessageTypeOperator, SentBy: conversation.SentByPerson("4b0f3e86-3a6e-4c43-9d0e-8b1b1f0b8c11"), SenderName: "Bruno Lima"}, AuthorTeam},
		"agent":    {conversation.Message{MessageType: conversation.MessageTypeAIResponse, SentBy: conversation.SentByAI("agent-1"), SenderName: "Bot"}, AuthorAssistant},
		"workflow": {conversation.Message{MessageType: conversation.MessageTypeAIResponse, SentBy: conversation.SentByWorkflow("wf-1")}, AuthorAssistant},
		"legacy":   {conversation.Message{MessageType: conversation.MessageTypeOperator}, AuthorTeam},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			tc.m.ID, tc.m.Text, tc.m.CreatedAt = "row-1", "olá", at
			got, ok := ProjectMessage(&tc.m)
			if !ok {
				t.Fatal("message dropped")
			}
			if got.Author != tc.wantAuthor || got.Text != "olá" || !got.CreatedAt.Equal(at) {
				t.Fatalf("projection = %+v", got)
			}
		})
	}
}

func TestProjectMessagePrefersTheProviderIDSoLiveAndHistoryAgree(t *testing.T) {
	m := &conversation.Message{ID: "row-1", ExternalMessageID: ptr("wc_out_1"), MessageType: conversation.MessageTypeOperator, Text: "x"}
	got, _ := ProjectMessage(m)
	if got.ID != "wc_out_1" {
		t.Fatalf("id = %q", got.ID)
	}
}

func TestProjectMessageCarriesMediaButNotItsLayoutInternals(t *testing.T) {
	m := &conversation.Message{ID: "r", MessageType: conversation.MessageTypeMedia, MediaType: conversation.MediaTypeImage,
		Media: &conversation.AttachedMedia{URL: "https://cdn/x.png", MimeType: "image/png", Filename: "x.png"}}
	got, ok := ProjectMessage(m)
	if !ok || got.Media == nil || got.Media.URL != "https://cdn/x.png" || got.Media.Kind != "image" {
		t.Fatalf("projection = %+v", got)
	}
}

func TestCleanVisitorText(t *testing.T) {
	if _, err := CleanVisitorText("   "); !errors.Is(err, ErrMessageEmpty) {
		t.Fatalf("blank = %v", err)
	}
	if _, err := CleanVisitorText(strings.Repeat("a", MaxVisitorTextRunes+1)); !errors.Is(err, ErrMessageTooLong) {
		t.Fatalf("long = %v", err)
	}
	got, err := CleanVisitorText(" linha 1\nlinha\x002\x1b ")
	if err != nil || got != "linha 1\nlinha2" {
		t.Fatalf("clean = %q %v", got, err)
	}
}

func TestInboundProviderIDIsScopedToTheVisitor(t *testing.T) {
	a, err := InboundProviderID("v1", "c-123456789")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := InboundProviderID("v2", "c-123456789")
	if a == b {
		t.Fatal("two visitors reusing a client id must not collide")
	}
	for _, bad := range []string{"", "short", strings.Repeat("a", 65), "has space 123", "semi;colon12"} {
		if _, err := InboundProviderID("v1", bad); !errors.Is(err, ErrClientMessageIDInvalid) {
			t.Errorf("client id %q accepted", bad)
		}
	}
}
