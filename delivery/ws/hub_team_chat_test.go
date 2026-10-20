package ws

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"vozko/domain/shared"
	"vozko/domain/teamchat"
)

type typingCall struct {
	by       shared.Person
	ws       string
	threadID string
}

type fakeTeamTyping struct {
	mu    sync.Mutex
	calls []typingCall
	done  chan struct{}
}

func (f *fakeTeamTyping) Typing(_ context.Context, by shared.Person, workspaceID, threadID string) error {
	f.mu.Lock()
	f.calls = append(f.calls, typingCall{by: by, ws: workspaceID, threadID: threadID})
	f.mu.Unlock()
	f.done <- struct{}{}
	return teamchat.ErrNotFound
}

var teamMessage = teamchat.AuthoredMessage{
	Message: teamchat.Message{
		ID: "m-1", ThreadID: "t-1", Seq: 4, AuthorID: "u-2", Kind: teamchat.MessageText, Body: "oi",
		Mentions: []string{"u-1"}, CreatedAt: time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC),
	},
	AuthorName: "Bia",
}

func receive(t *testing.T, conn *WSConnection) (string, json.RawMessage) {
	t.Helper()
	select {
	case raw := <-conn.Send:
		var msg struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(raw, &msg); err != nil {
			t.Fatal(err)
		}
		return msg.Type, msg.Payload
	default:
		t.Fatal("expected a push")
		return "", nil
	}
}

func TestTeamMessagesReachOnlyTheMembersInThatWorkspace(t *testing.T) {
	member := connectionFor("u-1", "ws-1")
	memberElsewhere := connectionFor("u-1", "ws-2")
	outsider := connectionFor("u-9", "ws-1")
	hub := notificationHub(map[string]*WSConnection{"a": member, "b": memberElsewhere, "c": outsider})

	hub.AnnounceTeamMessage("ws-1", []string{"u-1", "u-2"}, teamMessage)

	kind, payload := receive(t, member)
	var body struct {
		ThreadID string `json:"thread_id"`
		Message  struct {
			ID         string   `json:"id"`
			Seq        int64    `json:"seq"`
			AuthorName string   `json:"author_name"`
			Mentions   []string `json:"mentions"`
		} `json:"message"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatal(err)
	}
	if kind != "team_chat:message" || body.ThreadID != "t-1" || body.Message.Seq != 4 || body.Message.AuthorName != "Bia" || len(body.Message.Mentions) != 1 {
		t.Fatalf("push = %s %s", kind, payload)
	}
	if len(memberElsewhere.Send) != 0 || len(outsider.Send) != 0 {
		t.Fatal("only members connected to this workspace receive it")
	}
}

func TestEveryTeamChatEventHasItsOwnType(t *testing.T) {
	conn := &WSConnection{UserID: "u-1", WorkspaceID: "ws-1", Send: make(chan []byte, 8)}
	hub := notificationHub(map[string]*WSConnection{"a": conn})
	to := []string{"u-1"}
	hub.AnnounceTeamMessageChanged("ws-1", to, teamMessage)
	hub.AnnounceTeamRead("ws-1", to, teamchat.ReadMark{ThreadID: "t-1", UserID: "u-2", Seq: 4})
	hub.AnnounceTeamThread("ws-1", to, "t-1")
	hub.AnnounceTeamThreadRemoved("ws-1", to, "t-1")
	hub.AnnounceTeamTyping("ws-1", to, "t-1", "u-2")
	hub.AnnounceTeamScheduled("ws-1", "u-1", teamchat.Scheduled{ID: "s-1", ThreadID: "t-1", Status: teamchat.ScheduledSent})
	want := []string{"team_chat:message_updated", "team_chat:read", "team_chat:thread", "team_chat:thread_removed", "team_chat:typing", "team_chat:scheduled"}
	for _, w := range want {
		if kind, _ := receive(t, conn); kind != w {
			t.Fatalf("type = %s, want %s", kind, w)
		}
	}
}

func TestTeamChatPushesRelayedThroughRedisReachLocalMembers(t *testing.T) {
	conn := connectionFor("u-1", "ws-1")
	hub := notificationHub(map[string]*WSConnection{"a": conn})
	hub.deliverWorkspaceBroadcast(redisWorkspaceBroadcast{
		Type: teamChatBroadcastType, WorkspaceID: "ws-1", UserIDs: []string{"u-1"},
		Payload: json.RawMessage(`{"type":"team_chat:thread","payload":{"thread_id":"t-1"}}`), ReplicaID: "other",
	})
	if len(conn.Send) != 1 {
		t.Fatal("a relayed team chat push must reach the local member")
	}
}

func TestTeamChatAnnouncementsWithoutRecipientsSendNothing(t *testing.T) {
	conn := connectionFor("u-1", "ws-1")
	hub := notificationHub(map[string]*WSConnection{"a": conn})
	hub.AnnounceTeamThread("ws-1", nil, "t-1")
	var nilHub *ConversationHub
	nilHub.AnnounceTeamThread("ws-1", []string{"u-1"}, "t-1")
	if len(conn.Send) != 0 {
		t.Fatal("no recipients, no push")
	}
}

func TestTypingFromTheSocketGoesThroughTheTeamChatRules(t *testing.T) {
	typing := &fakeTeamTyping{done: make(chan struct{}, 1)}
	conn := connectionFor("u-1", "ws-1")
	hub := notificationHub(map[string]*WSConnection{"a": conn})
	hub.SetTeamChat(typing)
	hub.handleIncoming(&incomingMessage{conn: conn, message: &WSIncomingMessage{
		Type: WSEventTeamChatTyping, Payload: json.RawMessage(`{"thread_id":" t-1 "}`),
	}})
	select {
	case <-typing.done:
	case <-time.After(time.Second):
		t.Fatal("typing never reached the use case")
	}
	got := typing.calls[0]
	if got.by.UserID != "u-1" || got.ws != "ws-1" || got.threadID != "t-1" {
		t.Fatalf("call = %+v", got)
	}
	if len(conn.Send) != 0 {
		t.Fatal("a refused typing signal stays silent")
	}
}
