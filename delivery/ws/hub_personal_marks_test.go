package ws

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"vozko/domain/conversation"
	"vozko/domain/personalinbox"
	"vozko/domain/shared"
	personalinbox_usecase "vozko/usecases/personalinbox"
)

type personalCall struct {
	kind        string
	by          shared.Person
	workspaceID string
	entry       shared.EntryRef
	pinned      bool
}

type fakePersonalMarks struct {
	mu        sync.Mutex
	calls     chan personalCall
	err       error
	pageMarks []personalinbox.Mark
	pageAsked []shared.EntryRef
}

func newFakePersonalMarks() *fakePersonalMarks {
	return &fakePersonalMarks{calls: make(chan personalCall, 8)}
}

func (f *fakePersonalMarks) record(c personalCall) (personalinbox.Mark, error) {
	f.calls <- c
	if f.err != nil {
		return personalinbox.Mark{}, f.err
	}
	return personalinbox.NewMark(c.by.UserID, c.entry)
}

func (f *fakePersonalMarks) Pin(_ context.Context, by shared.Person, workspaceID string, entry shared.EntryRef, pinned bool) (personalinbox.Mark, error) {
	return f.record(personalCall{kind: "pin", by: by, workspaceID: workspaceID, entry: entry, pinned: pinned})
}

func (f *fakePersonalMarks) MarkUnread(_ context.Context, by shared.Person, workspaceID string, entry shared.EntryRef) (personalinbox.Mark, error) {
	return f.record(personalCall{kind: "unread", by: by, workspaceID: workspaceID, entry: entry})
}

func (f *fakePersonalMarks) ClearUnreadMark(_ context.Context, by shared.Person, workspaceID string, entry shared.EntryRef) (personalinbox.Mark, error) {
	return f.record(personalCall{kind: "clear_unread", by: by, workspaceID: workspaceID, entry: entry})
}

func (f *fakePersonalMarks) MarkRead(_ context.Context, by shared.Person, workspaceID string, entry shared.EntryRef) {
	f.calls <- personalCall{kind: "read", by: by, workspaceID: workspaceID, entry: entry}
}

func (f *fakePersonalMarks) PageMarks(_ context.Context, userID string, entries []shared.EntryRef) []personalinbox.Mark {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pageAsked = append([]shared.EntryRef(nil), entries...)
	return f.pageMarks
}

func awaitPersonalCall(t *testing.T, f *fakePersonalMarks) personalCall {
	t.Helper()
	select {
	case c := <-f.calls:
		return c
	case <-time.After(2 * time.Second):
		t.Fatal("the personal mark use case was never called")
		return personalCall{}
	}
}

func newPersonalHub(t *testing.T) (*ConversationHub, *WSConnection, *fakePersonalMarks) {
	t.Helper()
	hub, conn := newSendHub(t, panicSendTestUseCase{})
	marks := newFakePersonalMarks()
	hub.SetPersonalMarks(marks)
	return hub, conn, marks
}

func personalFrame(t *testing.T, fields map[string]any) json.RawMessage {
	t.Helper()
	base := map[string]any{"entry_id": "entry-1", "entry_type": "telegram", "request_id": "req-1"}
	for k, v := range fields {
		base[k] = v
	}
	raw, err := json.Marshal(base)
	require.NoError(t, err)
	return raw
}

var chatRef = shared.EntryRef{EntryID: "entry-1", EntryType: shared.EntryTypeTelegram}

var _ personalinbox_usecase.Announcer = (*ConversationHub)(nil)

func TestPersonalMarkCommandsReachTheirUseCaseAsTheConnectedOperator(t *testing.T) {
	cases := []struct {
		event WSEventType
		extra map[string]any
		want  personalCall
	}{
		{WSEventPinConversation, map[string]any{"pinned": true}, personalCall{kind: "pin", pinned: true}},
		{WSEventPinConversation, map[string]any{"pinned": false}, personalCall{kind: "pin", pinned: false}},
		{WSEventMarkUnread, nil, personalCall{kind: "unread"}},
	}
	for _, c := range cases {
		t.Run(string(c.event)+" "+c.want.kind, func(t *testing.T) {
			hub, conn, marks := newPersonalHub(t)
			hub.handleIncoming(&incomingMessage{conn: conn, message: &WSIncomingMessage{Type: c.event, Payload: personalFrame(t, c.extra)}})

			got := awaitPersonalCall(t, marks)
			require.Equal(t, c.want.kind, got.kind)
			require.Equal(t, shared.Person{UserID: "user-1"}, got.by)
			require.Equal(t, "ws-1", got.workspaceID)
			require.Equal(t, chatRef, got.entry)
			require.Equal(t, c.want.pinned, got.pinned)
		})
	}
}

func TestARefusedPersonalMarkAnswersWithTheContractCode(t *testing.T) {
	cases := []struct {
		name  string
		event WSEventType
		frame map[string]any
		err   error
		code  string
	}{
		{"no entry", WSEventPinConversation, map[string]any{"entry_id": "", "pinned": true}, nil, messageActionInvalid},
		{"no pinned flag", WSEventPinConversation, nil, nil, messageActionInvalid},
		{"cannot see it", WSEventMarkUnread, nil, conversation.ErrUnauthorized, messageActionNotAllowed},
		{"storage failed", WSEventMarkUnread, nil, errors.New("db down"), messageActionFailed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hub, conn, marks := newPersonalHub(t)
			marks.err = c.err
			hub.handleIncoming(&incomingMessage{conn: conn, message: &WSIncomingMessage{Type: c.event, Payload: personalFrame(t, c.frame)}})

			msg := awaitSendPayload(t, conn)
			require.Equal(t, WSEventError, msg.Type)
			payload := decodeErrorPayload(t, msg)
			require.Equal(t, c.code, payload.Code)
			require.Equal(t, "req-1", payload.RequestID)
			require.Equal(t, "telegram", payload.EntryType)
		})
	}
}

func TestPersonalMarksWithoutTheUseCaseAreUnsupported(t *testing.T) {
	hub, conn := newSendHub(t, panicSendTestUseCase{})
	hub.handleIncoming(&incomingMessage{conn: conn, message: &WSIncomingMessage{Type: WSEventMarkUnread, Payload: personalFrame(t, nil)}})
	payload := decodeErrorPayload(t, awaitSendPayload(t, conn))
	require.Equal(t, messageActionUnsupported, payload.Code)
}

func decodePersonalMark(t *testing.T, raw []byte) (WSEventType, map[string]json.RawMessage) {
	t.Helper()
	var msg struct {
		Type    WSEventType                `json:"type"`
		Payload map[string]json.RawMessage `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(raw, &msg))
	return msg.Type, msg.Payload
}

func TestAnnouncedPersonalMarkReachesOnlyThatUsersConnectionsInTheWorkspace(t *testing.T) {
	mine := connectionFor("u-1", "ws-1")
	myOtherTab := connectionFor("u-1", "ws-1")
	mineElsewhere := connectionFor("u-1", "ws-2")
	colleague := connectionFor("u-2", "ws-1")
	hub := notificationHub(map[string]*WSConnection{"a": mine, "b": myOtherTab, "c": mineElsewhere, "d": colleague})

	pinnedAt := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	mark, _ := personalinbox.NewMark("u-1", chatRef)
	mark.PinnedAt = &pinnedAt
	hub.AnnouncePersonalMark("ws-1", mark)

	for _, conn := range []*WSConnection{mine, myOtherTab} {
		require.Len(t, conn.Send, 1)
		kind, payload := decodePersonalMark(t, <-conn.Send)
		require.Equal(t, WSEventPersonalMark, kind)
		require.JSONEq(t, `"entry-1"`, string(payload["entry_id"]))
		require.JSONEq(t, `"telegram"`, string(payload["entry_type"]))
		require.JSONEq(t, `{"pinned_at":"2026-10-18T12:00:00Z"}`, string(payload["mark"]))
	}
	require.Empty(t, mineElsewhere.Send)
	require.Empty(t, colleague.Send, "a personal mark must never reach a teammate")
}

func TestAnEmptyPersonalMarkIsAnnouncedAsNull(t *testing.T) {
	mine := connectionFor("u-1", "ws-1")
	hub := notificationHub(map[string]*WSConnection{"a": mine})
	empty, _ := personalinbox.NewMark("u-1", chatRef)
	hub.AnnouncePersonalMark("ws-1", empty)
	_, payload := decodePersonalMark(t, <-mine.Send)
	require.Equal(t, "null", string(payload["mark"]))
}

func TestAPersonalMarkFromAnotherReplicaIsDeliveredLocally(t *testing.T) {
	mine := connectionFor("u-1", "ws-1")
	hub := notificationHub(map[string]*WSConnection{"a": mine})
	hub.deliverWorkspaceBroadcast(redisWorkspaceBroadcast{
		Type: personalMarkBroadcastType, WorkspaceID: "ws-1", UserIDs: []string{"u-1"},
		Payload: json.RawMessage(`{"type":"conversation:personal_mark","payload":{}}`), ReplicaID: "other",
	})
	require.Len(t, mine.Send, 1)
}

func TestOpeningAndReadingAConversationClearTheUnreadMark(t *testing.T) {
	t.Run("subscribe", func(t *testing.T) {
		hub, conn, marks := newPersonalHub(t)
		hub.handleSubscribe(conn, personalFrame(t, nil))
		got := awaitPersonalCall(t, marks)
		require.Equal(t, personalCall{kind: "read", by: shared.Person{UserID: "user-1"}, workspaceID: "ws-1", entry: chatRef}, got)
	})
	t.Run("mark_read", func(t *testing.T) {
		hub, conn, marks := newPersonalHub(t)
		hub.handleMarkRead(conn, personalFrame(t, map[string]any{"message_ids": []string{"m-1"}}))
		got := awaitPersonalCall(t, marks)
		require.Equal(t, "read", got.kind)
		require.Equal(t, chatRef, got.entry)
	})
}

func TestANewMessageLeavesPersonalMarksAlone(t *testing.T) {
	hub, _, marks := newPersonalHub(t)
	hub.BroadcastNewMessage("entry-1", "telegram", &conversation.Message{ID: "m-1", SentBy: conversation.SentByPerson("user-1")})
	hub.BroadcastNewMessage("entry-1", "telegram", &conversation.Message{ID: "m-2", SentBy: conversation.SentByContact("+55")})

	select {
	case call := <-marks.calls:
		t.Fatalf("a new message changed a personal mark: %+v", call)
	case <-time.After(100 * time.Millisecond):
	}
}

type capturingInbox struct {
	mu      sync.Mutex
	inputs  []conversation.SearchInboxInput
	entries []conversation.InboxEntry
}

func (c *capturingInbox) last() conversation.SearchInboxInput {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.inputs[len(c.inputs)-1]
}

func (c *capturingInbox) GetInboxPage(string, string, string, string, int, int, bool) ([]conversation.InboxEntry, int64, error) {
	return nil, 0, nil
}

func (c *capturingInbox) GetInboxPageByWorkspace(string, string, string, string, int, int, bool) ([]conversation.InboxEntry, int64, error) {
	return nil, 0, nil
}

func (c *capturingInbox) SearchInbox(_ string, input conversation.SearchInboxInput) ([]conversation.InboxEntry, int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inputs = append(c.inputs, input)
	return c.entries, int64(len(c.entries)), nil
}

func (c *capturingInbox) BuildInboxEntry(string, string) (*conversation.InboxEntry, error) {
	return nil, nil
}

func (c *capturingInbox) SendTemplateForEntry(string, string, string, []string, string, string, bool) (string, error) {
	return "", nil
}

func (c *capturingInbox) GetInboxStageCounts(string, string, string) (map[string]int64, error) {
	return nil, nil
}

func (c *capturingInbox) GetConversationStatusCounts(string, string, string) (map[string]int64, error) {
	return nil, nil
}

func (c *capturingInbox) GetAvailableLabels(string) ([]conversation.InboxEntryLabel, error) {
	return nil, nil
}

func newInboxHub(t *testing.T) (*ConversationHub, *WSConnection, *fakePersonalMarks, *capturingInbox) {
	t.Helper()
	hub, conn, marks := newPersonalHub(t)
	conn.ViewMode = "global"
	inbox := &capturingInbox{entries: []conversation.InboxEntry{{EntryID: "entry-1", EntryType: "telegram"}, {EntryID: "entry-2", EntryType: "telegram"}}}
	hub.SetInboxService(inbox)
	pinnedAt := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	pinned, _ := personalinbox.NewMark("user-1", chatRef)
	pinned.PinnedAt = &pinnedAt
	marks.pageMarks = []personalinbox.Mark{pinned}
	return hub, conn, marks, inbox
}

func awaitPayloadOf(t *testing.T, conn *WSConnection, kind WSEventType) map[string]json.RawMessage {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case raw := <-conn.Send:
			var msg struct {
				Type    WSEventType                `json:"type"`
				Payload map[string]json.RawMessage `json:"payload"`
			}
			require.NoError(t, json.Unmarshal(raw, &msg))
			if msg.Type == kind {
				return msg.Payload
			}
		case <-deadline:
			t.Fatalf("no %s arrived", kind)
			return nil
		}
	}
}

func TestSearchInboxMapsSortPersonalAndLabelAndAnswersWithTheRequestersMarks(t *testing.T) {
	hub, conn, marks, inbox := newInboxHub(t)
	label := "6f1b5a3e-9a59-4d5e-8a43-2d3f0c4b1e77"
	hub.handleSearchInbox(conn, personalFrame(t, map[string]any{"sort": "waiting", "personal": "pinned", "label_id": label}))

	payload := awaitPayloadOf(t, conn, WSEventSearchResults)
	input := inbox.last()
	require.Equal(t, conversation.SortOrderWaiting, input.SortOrder)
	require.Equal(t, personalinbox.ViewPinned, input.Personal)
	require.Equal(t, label, input.LabelID)
	require.JSONEq(t, `[{"entry_id":"entry-1","entry_type":"telegram","pinned_at":"2026-10-18T12:00:00Z"}]`, string(payload["personal_marks"]))
	require.JSONEq(t, `{"sort":"waiting","label_id":"`+label+`","personal":"pinned"}`, string(payload["filters"]))
	require.Equal(t, []shared.EntryRef{chatRef, {EntryID: "entry-2", EntryType: shared.EntryTypeTelegram}}, marks.pageAsked)
}

func TestSearchInboxWithoutPersonalIsTheInboxView(t *testing.T) {
	hub, conn, _, inbox := newInboxHub(t)
	hub.handleSearchInbox(conn, personalFrame(t, nil))
	awaitPayloadOf(t, conn, WSEventSearchResults)
	require.Equal(t, personalinbox.ViewInbox, inbox.last().Personal)
	require.Equal(t, conversation.SortOrderRecent, inbox.last().SortOrder)
}

func TestSearchInboxRefusesUnknownSortPersonalAndLabel(t *testing.T) {
	cases := map[string]struct {
		frame map[string]any
		code  string
	}{
		"sort":     {map[string]any{"sort": "oldest"}, "invalid_sort"},
		"personal": {map[string]any{"personal": "archived"}, "invalid_personal"},
		"label":    {map[string]any{"label_id": "not-a-label"}, "invalid_label"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			hub, conn, _, inbox := newInboxHub(t)
			hub.handleSearchInbox(conn, personalFrame(t, c.frame))
			payload := decodeErrorPayload(t, awaitSendPayload(t, conn))
			require.Equal(t, c.code, payload.Code)
			require.Empty(t, inbox.inputs, "a refused search must not run")
		})
	}
}

func TestInboxPagesAreTheRequestersInboxViewWithTheirMarks(t *testing.T) {
	hub, conn, _, inbox := newInboxHub(t)
	hub.handleRequestInboxPage(conn, json.RawMessage(`{"page":2}`))

	payload := awaitPayloadOf(t, conn, WSEventInbox)
	require.Equal(t, personalinbox.ViewInbox, inbox.last().Personal)
	require.Equal(t, 2, inbox.last().Page)
	require.JSONEq(t, `[{"entry_id":"entry-1","entry_type":"telegram","pinned_at":"2026-10-18T12:00:00Z"}]`, string(payload["personal_marks"]))
}

func TestClearUnreadMarkReachesOnlyItsUseCase(t *testing.T) {
	hub, conn, marks := newPersonalHub(t)
	hub.handleIncoming(&incomingMessage{conn: conn, message: &WSIncomingMessage{Type: WSEventClearUnreadMark, Payload: personalFrame(t, nil)}})

	got := awaitPersonalCall(t, marks)
	require.Equal(t, personalCall{kind: "clear_unread", by: shared.Person{UserID: "user-1"}, workspaceID: "ws-1", entry: chatRef}, got)
	select {
	case extra := <-marks.calls:
		t.Fatalf("undoing mark unread must not read the conversation: %+v", extra)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestARefusedClearUnreadMarkAnswersWithTheContractCode(t *testing.T) {
	hub, conn, marks := newPersonalHub(t)
	marks.err = conversation.ErrUnauthorized
	hub.handleIncoming(&incomingMessage{conn: conn, message: &WSIncomingMessage{Type: WSEventClearUnreadMark, Payload: personalFrame(t, nil)}})

	payload := decodeErrorPayload(t, awaitSendPayload(t, conn))
	require.Equal(t, messageActionNotAllowed, payload.Code)
	require.Equal(t, "req-1", payload.RequestID)
	require.Equal(t, "entry-1", payload.EntryID)
}

func TestClearUnreadMarkWithoutAnEntryIsInvalid(t *testing.T) {
	hub, conn, _ := newPersonalHub(t)
	raw, err := json.Marshal(map[string]any{"entry_id": "", "entry_type": "telegram", "request_id": "req-1"})
	require.NoError(t, err)
	hub.handleIncoming(&incomingMessage{conn: conn, message: &WSIncomingMessage{Type: WSEventClearUnreadMark, Payload: raw}})
	require.Equal(t, messageActionInvalid, decodeErrorPayload(t, awaitSendPayload(t, conn)).Code)
}
