package ws

import (
	"encoding/json"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type pagedHistoryReader struct {
	asked []conversation.HistoryQuery
	page  conversation.HistoryPage
	err   error
}

func (r *pagedHistoryReader) ReadHistory(q conversation.HistoryQuery) (conversation.HistoryPage, error) {
	r.asked = append(r.asked, q)
	return r.page, r.err
}

type aroundHistoryProvider struct {
	conversation.HistoryProvider
	presented []string
}

func (p *aroundHistoryProvider) GetHistoryAround(string, shared.EntryType, time.Time, int) ([]*conversation.Message, bool, bool, int64, error) {
	return []*conversation.Message{{ID: "m1"}, {ID: "m2"}}, true, true, 9, nil
}

func (p *aroundHistoryProvider) GetEntryInfo(string, string) (conversation.EntryInfo, error) {
	return conversation.EntryInfo{}, nil
}

func (p *aroundHistoryProvider) GetUnreadCount(string, shared.EntryType) (int64, error) {
	return 0, nil
}

func (p *aroundHistoryProvider) GetWindowStatusForEntry(string, string) conversation.WindowState {
	return conversation.WindowState{}
}

func (p *aroundHistoryProvider) PresentMessage(_, _ string, message *conversation.Message) {
	p.presented = append(p.presented, message.ID)
	message.SenderName = "Ana"
}

func historyHub(t *testing.T, allowed bool) (*ConversationHub, *WSConnection, *pagedHistoryReader) {
	t.Helper()
	auth := &hubDepartmentTestAuthorizer{entryAccess: map[string]bool{"user-1": allowed}}
	hub := NewConversationHub(auth, nil, nil, &eligibilityFakeSharedState{}, "test-replica", "")
	hub.SetWSMetrics(noopWSMetricsRecorder{})
	reader := &pagedHistoryReader{page: conversation.HistoryPage{Messages: []*conversation.Message{{ID: "m3"}, {ID: "m4"}}, HasAfter: true}}
	hub.SetHistoryReader(reader)
	hub.SetHistoryProvider(&aroundHistoryProvider{})
	conn := &WSConnection{ID: "conn-1", UserID: "user-1", WorkspaceID: "ws-1", Send: make(chan []byte, 16), Done: make(chan struct{})}
	hub.connections[conn.ID] = conn
	hub.userConnections[conn.UserID] = map[string]bool{conn.ID: true}
	return hub, conn, reader
}

func nextEvent(t *testing.T, conn *WSConnection, want WSEventType) map[string]any {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case raw := <-conn.Send:
			var event struct {
				Type    WSEventType    `json:"type"`
				Payload map[string]any `json:"payload"`
			}
			if err := json.Unmarshal(raw, &event); err != nil {
				t.Fatal(err)
			}
			if event.Type == want {
				return event.Payload
			}
		case <-deadline:
			t.Fatalf("no %s arrived", want)
		}
	}
}

func payloadOf(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestLoadAfterAnswersWithTheNextPageThroughTheAccessCheckedReader(t *testing.T) {
	hub, conn, reader := historyHub(t, true)
	after := "2026-10-18T12:00:00.123456789Z"

	hub.handleLoadAfter(conn, payloadOf(t, LoadAfterPayload{EntryID: "e1", EntryType: "whatsapp", After: after, PageSize: 500}))

	history := nextEvent(t, conn, WSEventHistory)
	if history["direction"] != "after" || history["has_after"] != true || history["has_more"] != false {
		t.Fatalf("history = %v", history)
	}
	if len(history["messages"].([]any)) != 2 {
		t.Fatalf("messages = %v", history["messages"])
	}
	if len(reader.asked) != 1 {
		t.Fatalf("reader asked %d times", len(reader.asked))
	}
	q := reader.asked[0]
	want, _ := time.Parse(time.RFC3339Nano, after)
	if q.After == nil || !q.After.Equal(want) || q.Before != nil || q.Limit != 500 || q.Viewer.UserID != "user-1" || q.Viewer.WorkspaceID != "ws-1" {
		t.Fatalf("query = %+v", q)
	}
}

func TestLoadAfterRefusesWhatTheViewerCannotSee(t *testing.T) {
	hub, conn, reader := historyHub(t, true)
	reader.err = conversation.ErrUnauthorized

	hub.handleLoadAfter(conn, payloadOf(t, LoadAfterPayload{EntryID: "e1", EntryType: "whatsapp", After: "2026-10-18T12:00:00Z"}))

	if got := nextEvent(t, conn, WSEventError); got["code"] != "unauthorized" {
		t.Fatalf("error = %v", got)
	}
}

func TestLoadAfterRefusesAMissingOrInvalidCursor(t *testing.T) {
	for name, p := range map[string]LoadAfterPayload{
		"no cursor":  {EntryID: "e1", EntryType: "whatsapp"},
		"bad cursor": {EntryID: "e1", EntryType: "whatsapp", After: "yesterday"},
		"no entry":   {EntryType: "whatsapp", After: "2026-10-18T12:00:00Z"},
	} {
		t.Run(name, func(t *testing.T) {
			hub, conn, reader := historyHub(t, true)
			hub.handleLoadAfter(conn, payloadOf(t, p))
			if got := nextEvent(t, conn, WSEventError); got["code"] == "" {
				t.Fatalf("error = %v", got)
			}
			if len(reader.asked) != 0 {
				t.Fatal("history must not be read for an invalid request")
			}
		})
	}
}

func TestEveryHistoryPageSaysWhichWayItReads(t *testing.T) {
	hub, conn, _ := historyHub(t, true)

	hub.handleLoadHistory(conn, payloadOf(t, LoadHistoryPayload{EntryID: "e1", EntryType: "whatsapp", Before: "2026-10-18T12:00:00Z"}))
	if got := nextEvent(t, conn, WSEventHistory); got["direction"] != "before" {
		t.Fatalf("load_history direction = %v", got["direction"])
	}

	hub.handleLoadAround(conn, payloadOf(t, LoadAroundPayload{EntryID: "e1", EntryType: "whatsapp", Timestamp: "2026-10-18T12:00:00Z"}))
	around := nextEvent(t, conn, WSEventHistory)
	if around["direction"] != "around" || around["has_after"] != true || around["has_more"] != true {
		t.Fatalf("load_around = %v", around)
	}

	hub.handleSubscribe(conn, payloadOf(t, SubscribePayload{EntryID: "e1", EntryType: "whatsapp"}))
	if got := nextEvent(t, conn, WSEventHistory); got["direction"] != "latest" {
		t.Fatalf("subscribe direction = %v", got["direction"])
	}
}

func TestMessageUpdatedReachesOnlyTheEntrysSubscribersPresented(t *testing.T) {
	hub, subscriber, _ := historyHub(t, true)
	provider := &aroundHistoryProvider{}
	hub.SetHistoryProvider(provider)
	bystander := &WSConnection{ID: "conn-2", UserID: "user-2", WorkspaceID: "ws-1", Send: make(chan []byte, 16), Done: make(chan struct{})}
	hub.connections[bystander.ID] = bystander
	hub.userConnections[bystander.UserID] = map[string]bool{bystander.ID: true}
	sub := entrySubscription{entryID: "e1", entryType: "whatsapp"}
	other := entrySubscription{entryID: "e2", entryType: "whatsapp"}
	hub.entrySubscribers[sub] = map[string]bool{subscriber.ID: true}
	hub.entrySubscribers[other] = map[string]bool{bystander.ID: true}

	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	edited := &conversation.Message{ID: "m1", EntryID: "e1", Text: "olá"}
	edited.ApplyEdit("olá de novo", at)
	hub.BroadcastMessageUpdated("e1", "whatsapp", edited)
	hub.handleBroadcast(<-hub.broadcast)

	got := nextEvent(t, subscriber, WSEventMessageUpdated)
	message := got["message"].(map[string]any)
	if got["entry_id"] != "e1" || got["entry_type"] != "whatsapp" || message["text"] != "olá de novo" || message["edited_at"] != "2026-10-18T12:00:00Z" {
		t.Fatalf("payload = %v", got)
	}
	if message["senderName"] != "Ana" || len(provider.presented) != 1 {
		t.Fatalf("the update must go through the live presenter, got %v", message)
	}
	select {
	case raw := <-bystander.Send:
		t.Fatalf("a connection not subscribed to the entry received %s", raw)
	default:
	}
}

type anchorHistoryProvider struct {
	aroundHistoryProvider
	anchors []time.Time
}

func (p *anchorHistoryProvider) GetHistoryAround(_ string, _ shared.EntryType, around time.Time, _ int) ([]*conversation.Message, bool, bool, int64, error) {
	p.anchors = append(p.anchors, around)
	return []*conversation.Message{{ID: "quoted"}}, false, true, 3, nil
}

type quotedMessages struct {
	conversation.MessageRepository
	byID map[string]*conversation.Message
}

func (q quotedMessages) GetByID(id string) (*conversation.Message, error) {
	if m, ok := q.byID[id]; ok {
		return m, nil
	}
	return nil, conversation.ErrMessageNotFound
}

func quoteHub(t *testing.T, allowed bool) (*ConversationHub, *WSConnection, *anchorHistoryProvider) {
	t.Helper()
	hub, conn, _ := historyHub(t, allowed)
	provider := &anchorHistoryProvider{}
	hub.SetHistoryProvider(provider)
	at := time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)
	hub.SetMessageRepo(quotedMessages{byID: map[string]*conversation.Message{
		"quoted":  {ID: "quoted", EntryID: "e1", EntryType: shared.EntryTypeWhatsApp, CreatedAt: at},
		"foreign": {ID: "foreign", EntryID: "e2", EntryType: shared.EntryTypeWhatsApp, CreatedAt: at},
	}})
	return hub, conn, provider
}

func TestLoadAroundAQuotedMessageAnchorsOnItsSendTime(t *testing.T) {
	hub, conn, provider := quoteHub(t, true)

	hub.handleLoadAround(conn, payloadOf(t, LoadAroundPayload{EntryID: "e1", EntryType: "whatsapp", MessageID: "quoted"}))

	history := nextEvent(t, conn, WSEventHistory)
	if history["direction"] != "around" || history["has_after"] != true {
		t.Fatalf("history = %v", history)
	}
	if len(provider.anchors) != 1 || !provider.anchors[0].Equal(time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)) {
		t.Fatalf("anchors = %v, want the quoted message's createdAt", provider.anchors)
	}
}

func TestLoadAroundRefusesAMessageOfAnotherConversation(t *testing.T) {
	for _, id := range []string{"foreign", "missing"} {
		t.Run(id, func(t *testing.T) {
			hub, conn, provider := quoteHub(t, true)

			hub.handleLoadAround(conn, payloadOf(t, LoadAroundPayload{EntryID: "e1", EntryType: "whatsapp", MessageID: id}))

			got := nextEvent(t, conn, WSEventError)
			if got["code"] != "not_found" || got["entry_id"] != "e1" || got["entry_type"] != "whatsapp" {
				t.Fatalf("error = %v", got)
			}
			if len(provider.anchors) != 0 {
				t.Fatal("history was read around a message the entry does not have")
			}
		})
	}
}

func TestLoadAroundAQuotedMessageChecksAccessFirst(t *testing.T) {
	hub, conn, provider := quoteHub(t, false)

	hub.handleLoadAround(conn, payloadOf(t, LoadAroundPayload{EntryID: "e1", EntryType: "whatsapp", MessageID: "quoted"}))

	if got := nextEvent(t, conn, WSEventError); got["code"] != "unauthorized" {
		t.Fatalf("error = %v", got)
	}
	if len(provider.anchors) != 0 {
		t.Fatal("history was read without access")
	}
}

func TestLoadAroundTakesExactlyOneAnchor(t *testing.T) {
	for name, p := range map[string]LoadAroundPayload{
		"neither": {EntryID: "e1", EntryType: "whatsapp"},
		"both":    {EntryID: "e1", EntryType: "whatsapp", Timestamp: "2026-10-18T12:00:00Z", MessageID: "quoted"},
	} {
		t.Run(name, func(t *testing.T) {
			hub, conn, provider := quoteHub(t, true)
			hub.handleLoadAround(conn, payloadOf(t, p))
			if got := nextEvent(t, conn, WSEventError); got["code"] == "" {
				t.Fatalf("error = %v", got)
			}
			if len(provider.anchors) != 0 {
				t.Fatal("history was read for an ambiguous anchor")
			}
		})
	}
}
