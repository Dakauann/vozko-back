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
	adapter := NewChannelAdapter(widgets, visitors, conversations, events)
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
