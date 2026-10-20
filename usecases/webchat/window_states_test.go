package webchat

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/conversation"
	wcdomain "vozko/domain/webchat"
)

type windowHarness struct {
	adapter       conversation.ChannelAdapter
	widgets       *fakeWidgets
	visitors      *fakeVisitors
	conversations *fakeConversations
}

func newWindowHarness() windowHarness {
	widgets := &fakeWidgets{byID: map[string]*wcdomain.Widget{"w1": {ID: "w1", WorkspaceID: "ws-1"}}}
	visitors := &fakeVisitors{byID: map[string]*wcdomain.Visitor{"v1": {ID: "v1", WidgetID: "w1"}, "v2": {ID: "v2", WidgetID: "w1"}}}
	conversations := &fakeConversations{byID: map[string]*wcdomain.Conversation{
		"c1": {ID: "c1", WorkspaceID: "ws-1", WidgetID: "w1", VisitorID: "v1"},
		"c2": {ID: "c2", WorkspaceID: "ws-1", WidgetID: "w1", VisitorID: "v2"},
	}}
	return windowHarness{
		adapter:       NewChannelAdapter(widgets, visitors, conversations, &fakeEvents{}, quotedLines),
		widgets:       widgets,
		visitors:      visitors,
		conversations: conversations,
	}
}

func (h windowHarness) batch(t *testing.T) conversation.WindowStatesAdapter {
	t.Helper()
	batch, ok := h.adapter.(conversation.WindowStatesAdapter)
	if !ok {
		t.Fatal("the webchat adapter must report window states for a page of entries")
	}
	return batch
}

func TestWindowStatesMatchTheSingleEntryRule(t *testing.T) {
	cases := []struct {
		name       string
		edit       func(windowHarness)
		wantOpen   bool
		wantReason conversation.WindowClosedReason
	}{
		{name: "open", wantOpen: true},
		{name: "visitor blocked", edit: func(h windowHarness) { h.visitors.byID["v1"].Blocked = true }, wantReason: conversation.WindowReasonContactBlocked},
		{name: "widget gone", edit: func(h windowHarness) { delete(h.widgets.byID, "w1") }, wantReason: conversation.WindowReasonChannelUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newWindowHarness()
			ctx := context.Background()
			ec, err := h.adapter.ResolveEntry(ctx, "c1")
			if err != nil {
				t.Fatal(err)
			}
			if tc.edit != nil {
				tc.edit(h)
			}

			single, err := h.adapter.WindowState(ctx, ec)
			if err != nil {
				t.Fatal(err)
			}
			states, err := h.batch(t).WindowStates(ctx, []string{"c1"})
			if err != nil {
				t.Fatal(err)
			}
			got, ok := states["c1"]
			if !ok {
				t.Fatal("no state for c1")
			}
			if got.Open != tc.wantOpen || got.Reason != tc.wantReason {
				t.Fatalf("batch = %+v, want open=%v reason=%q", got, tc.wantOpen, tc.wantReason)
			}
			if !got.Equal(single) {
				t.Fatalf("batch %+v disagrees with the single entry %+v", got, single)
			}
		})
	}
}

func TestWindowStatesLoadAPageInOneBatchPerTable(t *testing.T) {
	h := newWindowHarness()

	states, err := h.batch(t).WindowStates(context.Background(), []string{"c1", "c2", "c1", "gone"})
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 2 || !states["c1"].Open || !states["c2"].Open {
		t.Fatalf("states = %+v", states)
	}
	if h.conversations.findByIDsCalls != 1 || h.widgets.findByIDsCalls != 1 || h.visitors.findByIDsCalls != 1 {
		t.Fatalf("batches: conversations %d, widgets %d, visitors %d, want 1 each",
			h.conversations.findByIDsCalls, h.widgets.findByIDsCalls, h.visitors.findByIDsCalls)
	}
	if h.conversations.findByIDCalls != 0 || h.widgets.findByIDCalls != 0 || h.visitors.findByIDCalls != 0 {
		t.Fatalf("per row lookups: conversations %d, widgets %d, visitors %d",
			h.conversations.findByIDCalls, h.widgets.findByIDCalls, h.visitors.findByIDCalls)
	}
}

func TestWindowStatesCloseEntriesWithAMissingVisitor(t *testing.T) {
	h := newWindowHarness()
	delete(h.visitors.byID, "v1")

	states, err := h.batch(t).WindowStates(context.Background(), []string{"c1"})
	if err != nil {
		t.Fatal(err)
	}
	if got := states["c1"]; got.Open || got.Reason != conversation.WindowReasonChannelUnavailable {
		t.Fatalf("state = %+v, want closed channel_unavailable", got)
	}
}

func TestWindowStatesReturnLookupErrors(t *testing.T) {
	boom := errors.New("db down")
	for _, broken := range []string{"conversations", "widgets", "visitors"} {
		t.Run(broken, func(t *testing.T) {
			h := newWindowHarness()
			switch broken {
			case "conversations":
				h.conversations.findByIDsErr = boom
			case "widgets":
				h.widgets.findByIDsErr = boom
			default:
				h.visitors.findByIDsErr = boom
			}
			if _, err := h.batch(t).WindowStates(context.Background(), []string{"c1"}); !errors.Is(err, boom) {
				t.Fatalf("err = %v, want the lookup error", err)
			}
		})
	}
}
