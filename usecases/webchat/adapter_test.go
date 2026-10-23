package webchat

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	wcdomain "vozko/domain/webchat"
)

func adapterHarness() (conversation.ChannelAdapter, *fakeVisitors, *fakeConversations, *fakeEvents, *conversation.EntryContext) {
	widgets := &fakeWidgets{byID: map[string]*wcdomain.Widget{"w1": {ID: "w1", WorkspaceID: "ws-1"}}}
	visitors := &fakeVisitors{byID: map[string]*wcdomain.Visitor{"v1": {ID: "v1", WidgetID: "w1"}}}
	conversations := &fakeConversations{byID: map[string]*wcdomain.Conversation{"c1": {ID: "c1", WorkspaceID: "ws-1", WidgetID: "w1", VisitorID: "v1"}}}
	events := &fakeEvents{}
	adapter := NewChannelAdapter(widgets, visitors, conversations, events, quotedLines)
	ec, _ := adapter.ResolveEntry(context.Background(), "c1")
	return adapter, visitors, conversations, events, ec
}

func TestAdapterDeliversToTheVisitorItResolved(t *testing.T) {
	adapter, _, _, events, ec := adapterHarness()
	if ec.EntryType != shared.EntryTypeWebchat || ec.ContactID != "v1" || ec.AccountID != "w1" {
		t.Fatalf("entry context = %+v", ec)
	}
	out, err := adapter.SendText(context.Background(), ec, conversation.SendTextRequest{Body: "Olá!", HumanInitiated: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(events.events) != 1 {
		t.Fatal("no live event")
	}
	ev := events.events[0]
	if ev.VisitorID != "v1" || ev.Message.ID != out.ProviderMessageID || ev.Message.Author != wcdomain.AuthorTeam {
		t.Fatalf("event = %+v", ev)
	}
}

func TestAutomatedRepliesAreLabelledAsTheAssistant(t *testing.T) {
	adapter, _, _, events, ec := adapterHarness()
	if _, err := adapter.SendText(context.Background(), ec, conversation.SendTextRequest{Body: "Olá!"}); err != nil {
		t.Fatal(err)
	}
	if events.events[0].Message.Author != wcdomain.AuthorAssistant {
		t.Fatalf("author = %q", events.events[0].Message.Author)
	}
}

func TestBlockedVisitorClosesTheComposer(t *testing.T) {
	adapter, visitors, _, _, ec := adapterHarness()
	visitors.byID["v1"].Blocked = true
	state, err := adapter.WindowState(context.Background(), ec)
	if err != nil {
		t.Fatal(err)
	}
	if state.Open || state.Reason != conversation.WindowReasonContactBlocked {
		t.Fatalf("window = %+v", state)
	}
}

func TestInteractivePromptRemembersTheOfferedOptions(t *testing.T) {
	adapter, _, conversations, events, ec := adapterHarness()
	interactive := adapter.(conversation.InteractiveAdapter)
	_, err := interactive.SendInteractive(context.Background(), ec, conversation.SendInteractiveRequest{
		Body:    "Com quem quer falar?",
		Options: []conversation.InteractiveOption{{ID: "sales", Title: "Vendas"}, {ID: "support", Title: "Suporte"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(conversations.byID["c1"].PendingOptions) != 2 || len(events.events[0].Message.Options) != 2 {
		t.Fatalf("options stored %v sent %v", conversations.byID["c1"].PendingOptions, events.events[0].Message.Options)
	}
}

func TestEmptyRepliesAreRefused(t *testing.T) {
	adapter, _, _, _, ec := adapterHarness()
	if _, err := adapter.SendText(context.Background(), ec, conversation.SendTextRequest{Body: "  "}); !errors.Is(err, wcdomain.ErrMessageEmpty) {
		t.Fatalf("empty reply = %v", err)
	}
}

func TestModerationNeedsEntryAccess(t *testing.T) {
	_, visitors, conversations, events, _ := adapterHarness()
	denied := NewModeration(conversations, visitors, fakeAccess(false), events, &fakeOperators{})
	if err := denied.SetBlocked(context.Background(), shared.Person{UserID: "u1"}, "ws-1", "c1", true); !errors.Is(err, wcdomain.ErrConversationNotFound) {
		t.Fatalf("block without access = %v", err)
	}
	if visitors.byID["v1"].Blocked {
		t.Fatal("visitor was blocked without access")
	}

	allowed := NewModeration(conversations, visitors, fakeAccess(true), events, &fakeOperators{})
	if err := allowed.SetBlocked(context.Background(), shared.Person{UserID: "u1"}, "ws-1", "c1", true); err != nil {
		t.Fatal(err)
	}
	if !visitors.byID["v1"].Blocked || events.events[0].State != wcdomain.StateBlocked {
		t.Fatal("block did not take effect")
	}
}

type fakeQuotedMessages map[string]*conversation.Message

func (f fakeQuotedMessages) GetByEntryAndExternalMessageID(entryType shared.EntryType, entryID, externalID string) (*conversation.Message, error) {
	if m, ok := f[string(entryType)+"|"+entryID+"|"+externalID]; ok {
		return m, nil
	}
	return nil, errors.New("message not found")
}

var quotedLines = fakeQuotedMessages{
	"webchat|c1|wc_in_v1_question1": {ID: "m-q", ExternalMessageID: ptrTo("wc_in_v1_question1"), MessageType: conversation.MessageTypeUserMessage, SentBy: conversation.SentByContact("v1"), Text: "Vocês entregam no sábado?"},
	"webchat|c1|wc_tool_internal1":  {ID: "m-t", ExternalMessageID: ptrTo("wc_tool_internal1"), MessageType: conversation.MessageTypeToolResult, Text: "segredo"},
}

func ptrTo(s string) *string { return &s }

func TestAReplyCarriesTheQuotedLineToTheVisitor(t *testing.T) {
	adapter, _, _, events, ec := adapterHarness()
	if _, err := adapter.SendText(context.Background(), ec, conversation.SendTextRequest{Body: "Entregamos sim", ReplyToProviderMessageID: "wc_in_v1_question1", HumanInitiated: true}); err != nil {
		t.Fatal(err)
	}
	quote := events.events[0].Message.Quote
	if quote == nil || quote.ID != "wc_in_v1_question1" || quote.Text != "Vocês entregam no sábado?" {
		t.Fatalf("quote = %+v", quote)
	}
}

func TestAMediaReplyCarriesTheQuotedLineToo(t *testing.T) {
	adapter, _, _, events, ec := adapterHarness()
	if _, err := adapter.SendMedia(context.Background(), ec, conversation.SendMediaRequest{Kind: "image", URL: "https://cdn.example/a.png", ReplyToProviderMessageID: "wc_in_v1_question1", HumanInitiated: true}); err != nil {
		t.Fatal(err)
	}
	if quote := events.events[0].Message.Quote; quote == nil || quote.ID != "wc_in_v1_question1" {
		t.Fatalf("quote = %+v", quote)
	}
}

func TestAReplyToWhatTheVisitorCannotSeeGoesOutWithoutAQuote(t *testing.T) {
	cases := map[string]string{
		"an internal message":               "wc_tool_internal1",
		"a message of another conversation": "wc_in_v9_elsewhere",
		"no quoted message":                 "",
	}
	for name, providerID := range cases {
		t.Run(name, func(t *testing.T) {
			adapter, _, _, events, ec := adapterHarness()
			if _, err := adapter.SendText(context.Background(), ec, conversation.SendTextRequest{Body: "Oi", ReplyToProviderMessageID: providerID, HumanInitiated: true}); err != nil {
				t.Fatalf("the reply itself must still be delivered: %v", err)
			}
			if quote := events.events[0].Message.Quote; quote != nil {
				t.Fatalf("quote = %+v, want none", quote)
			}
		})
	}
}

func TestThePromptOutcomeNamesTheOptionsTheVisitorSees(t *testing.T) {
	adapter, _, _, _, ec := adapterHarness()
	out, err := adapter.(conversation.InteractiveAdapter).SendInteractive(context.Background(), ec, conversation.SendInteractiveRequest{
		Body:    "Com quem quer falar?",
		Options: []conversation.InteractiveOption{{ID: "sales", Title: "Vendas"}, {ID: "", Title: "Sem id"}, {ID: "support", Title: "Suporte"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.ShownOptionIDs) != 2 || out.ShownOptionIDs[0] != "sales" || out.ShownOptionIDs[1] != "support" {
		t.Fatalf("shown = %v", out.ShownOptionIDs)
	}
}
