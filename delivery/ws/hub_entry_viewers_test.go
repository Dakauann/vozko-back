package ws

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"vozko/domain/user"
)

type setSharedState struct {
	eligibilityFakeSharedState
	mu        sync.Mutex
	sets      map[string]map[string]bool
	alive     map[string]bool
	expiries  map[string]time.Duration
	published int
}

func newSetSharedState(aliveKeys ...string) *setSharedState {
	s := &setSharedState{sets: map[string]map[string]bool{}, alive: map[string]bool{}, expiries: map[string]time.Duration{}}
	for _, k := range aliveKeys {
		s.alive[k] = true
	}
	return s
}

func (s *setSharedState) SAdd(key string, members ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sets[key] == nil {
		s.sets[key] = map[string]bool{}
	}
	for _, m := range members {
		s.sets[key][m] = true
	}
	return nil
}

func (s *setSharedState) SRem(key string, members ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range members {
		delete(s.sets[key], m)
	}
	return nil
}

func (s *setSharedState) SMembers(key string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.sets[key]))
	for m := range s.sets[key] {
		out = append(out, m)
	}
	sort.Strings(out)
	return out, nil
}

func (s *setSharedState) Exists(key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.alive[key], nil
}

func (s *setSharedState) Expire(key string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expiries[key] = ttl
	return true, nil
}

func (s *setSharedState) Publish(string, []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.published++
	return nil
}

func (s *setSharedState) members(key string) string {
	got, _ := s.SMembers(key)
	return strings.Join(got, ",")
}

type namedUsers struct {
	user.UserRepository
	names map[string]string
}

func (n namedUsers) FindByIDs(ids []string) ([]*user.User, error) {
	var out []*user.User
	for _, id := range ids {
		if name, ok := n.names[id]; ok {
			out = append(out, &user.User{ID: id, Username: name})
		}
	}
	return out, nil
}

const viewedKey = "hub:viewers:whatsapp:e-1"

var viewedEntry = entrySubscription{entryID: "e-1", entryType: "whatsapp"}

func viewersHub(t *testing.T, state *setSharedState) *ConversationHub {
	t.Helper()
	users := namedUsers{names: map[string]string{"u-ana": "Ana", "u-bia": "Bia", "u-rui": "Rui"}}
	hub := NewConversationHub(&hubDepartmentTestAuthorizer{entryAccess: map[string]bool{"u-ana": true, "u-bia": true, "u-new": true}}, users, nil, state, "replica-1", "")
	hub.viewersDelay = 0
	for _, c := range []struct{ id, user string }{
		{"c-ana", "u-ana"},
		{"c-ana-popout", "u-ana"},
		{"c-bia", "u-bia"},
		{"c-outsider", "u-outsider"},
	} {
		conn := &WSConnection{ID: c.id, UserID: c.user, WorkspaceID: "ws-1", Send: make(chan []byte, 16), Done: make(chan struct{})}
		hub.connections[c.id] = conn
		if hub.userConnections[c.user] == nil {
			hub.userConnections[c.user] = map[string]bool{}
		}
		hub.userConnections[c.user][c.id] = true
		hub.connSubscriptions[c.id] = map[entrySubscription]bool{}
	}
	for _, id := range []string{"c-ana", "c-ana-popout", "c-bia"} {
		hub.connSubscriptions[id][viewedEntry] = true
	}
	hub.entrySubscribers[viewedEntry] = map[string]bool{"c-ana": true, "c-ana-popout": true, "c-bia": true}
	return hub
}

type viewersFrame struct {
	Type    WSEventType `json:"type"`
	Payload struct {
		EntryID   string `json:"entry_id"`
		EntryType string `json:"entry_type"`
		Viewers   []struct {
			UserID string `json:"user_id"`
			Name   string `json:"name"`
		} `json:"viewers"`
	} `json:"payload"`
}

func viewersOf(t *testing.T, conn *WSConnection) []viewersFrame {
	t.Helper()
	var frames []viewersFrame
	for _, raw := range drained(conn) {
		var f viewersFrame
		if err := json.Unmarshal([]byte(raw), &f); err != nil {
			t.Fatal(err)
		}
		if f.Type == WSEventViewers {
			frames = append(frames, f)
		}
	}
	return frames
}

func (f viewersFrame) list() string {
	parts := make([]string, len(f.Payload.Viewers))
	for i, v := range f.Payload.Viewers {
		parts[i] = v.UserID + "=" + v.Name
	}
	return strings.Join(parts, ",")
}

func TestViewerUserIDsCountEachPersonOnceAndTrustOnlyLiveReplicas(t *testing.T) {
	alive := map[string]bool{"replica-2": true}
	users, stale := viewerUserIDs(
		[]string{"u-ana|replica-1", "u-gone|replica-1", "u-rui|replica-2", "u-ana|replica-2", "u-dead|replica-9", "garbage"},
		"replica-1",
		[]string{"u-bia", "u-ana", "u-bia"},
		func(replica string) bool { return alive[replica] },
	)
	if strings.Join(users, ",") != "u-ana,u-bia,u-rui" {
		t.Fatalf("users = %v; this replica answers for itself, repeated people count once, dead replicas are ignored", users)
	}
	if strings.Join(stale, ",") != "u-dead|replica-9" {
		t.Fatalf("stale = %v, want only the member of the dead replica", stale)
	}
}

func TestAnnouncedViewersReachEverySubscriberWithNamesAndNoOneElse(t *testing.T) {
	state := newSetSharedState("replica:heartbeat:replica-2")
	state.sets[viewedKey] = map[string]bool{"u-rui|replica-2": true, "u-dead|replica-9": true}
	hub := viewersHub(t, state)

	hub.announceViewers(viewedEntry)
	deliverQueuedBroadcast(t, hub)

	for _, id := range []string{"c-ana", "c-ana-popout", "c-bia"} {
		frames := viewersOf(t, hub.connections[id])
		if len(frames) != 1 {
			t.Fatalf("%s got %d viewer frames, want 1", id, len(frames))
		}
		f := frames[0]
		if f.Payload.EntryID != "e-1" || f.Payload.EntryType != "whatsapp" {
			t.Fatalf("frame = %+v", f)
		}
		if f.list() != "u-ana=Ana,u-bia=Bia,u-rui=Rui" {
			t.Fatalf("%s saw %s; Ana counts once across her tab and pop-out, Rui comes from the other replica", id, f.list())
		}
	}
	if frames := viewersOf(t, hub.connections["c-outsider"]); len(frames) != 0 {
		t.Fatal("someone not subscribed to the conversation must never learn who views it")
	}
	if got := state.members(viewedKey); got != "u-ana|replica-1,u-bia|replica-1,u-rui|replica-2" {
		t.Fatalf("shared viewers = %s; this replica publishes its viewers and the dead replica's member is cleaned up", got)
	}
	if state.expiries[viewedKey] <= 0 {
		t.Fatal("the shared viewer set must expire if this replica dies without cleaning up")
	}
	if state.published != 1 {
		t.Fatalf("published %d times, want 1 so the other replicas deliver it to their subscribers", state.published)
	}
}

func TestAPersonLeavesTheViewersOnlyWhenTheirLastConnectionUnsubscribes(t *testing.T) {
	state := newSetSharedState()
	hub := viewersHub(t, state)
	hub.announceViewers(viewedEntry)
	deliverQueuedBroadcast(t, hub)
	for id := range hub.connections {
		drained(hub.connections[id])
	}

	unsubscribe := func(conn string) {
		raw, _ := json.Marshal(SubscribePayload{EntryID: "e-1", EntryType: "whatsapp"})
		hub.handleUnsubscribe(hub.connections[conn], raw)
		deliverQueuedBroadcast(t, hub)
	}

	unsubscribe("c-ana-popout")
	if got := viewersOf(t, hub.connections["c-bia"]); len(got) != 1 || got[0].list() != "u-ana=Ana,u-bia=Bia" {
		t.Fatalf("after closing the pop-out Ana still views from her tab, got %+v", got)
	}
	if got := viewersOf(t, hub.connections["c-ana-popout"]); len(got) != 0 {
		t.Fatal("the connection that left is no longer a subscriber")
	}

	unsubscribe("c-ana")
	if got := viewersOf(t, hub.connections["c-bia"]); len(got) != 1 || got[0].list() != "u-bia=Bia" {
		t.Fatalf("after her last connection left, got %+v", got)
	}
	if got := state.members(viewedKey); got != "u-bia|replica-1" {
		t.Fatalf("shared viewers = %s, want Ana removed", got)
	}
}

func TestADisconnectAnnouncesTheViewersToWhoRemains(t *testing.T) {
	state := newSetSharedState()
	hub := viewersHub(t, state)
	hub.wsMetrics = noopWSMetricsRecorder{}

	hub.handleUnregister(hub.connections["c-bia"])
	deliverQueuedBroadcast(t, hub)

	got := viewersOf(t, hub.connections["c-ana"])
	if len(got) != 1 || got[0].list() != "u-ana=Ana" {
		t.Fatalf("remaining subscriber saw %+v", got)
	}
}

func TestANewSubscriberReceivesTheCurrentViewers(t *testing.T) {
	state := newSetSharedState()
	hub := viewersHub(t, state)
	conn := &WSConnection{ID: "c-new", UserID: "u-new", WorkspaceID: "ws-1", Send: make(chan []byte, 16), Done: make(chan struct{})}
	hub.connections[conn.ID] = conn
	hub.userConnections[conn.UserID] = map[string]bool{conn.ID: true}

	raw, _ := json.Marshal(SubscribePayload{EntryID: "e-1", EntryType: "whatsapp"})
	hub.handleSubscribe(conn, raw)
	deliverQueuedBroadcast(t, hub)

	got := viewersOf(t, conn)
	if len(got) != 1 || got[0].list() != "u-ana=Ana,u-bia=Bia,u-new=" {
		t.Fatalf("new subscriber saw %+v; everyone is listed, the client filters itself out", got)
	}
}

func TestARefusedSubscribeAnnouncesNothing(t *testing.T) {
	hub := viewersHub(t, newSetSharedState())
	raw, _ := json.Marshal(SubscribePayload{EntryID: "e-1", EntryType: "whatsapp"})
	hub.handleSubscribe(hub.connections["c-outsider"], raw)

	select {
	case msg := <-hub.broadcast:
		t.Fatalf("a refused subscribe queued %+v", msg.event)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestTheHeartbeatKeepsThisReplicasViewerSetsAlive(t *testing.T) {
	state := newSetSharedState()
	hub := viewersHub(t, state)
	hub.announceViewers(viewedEntry)
	delete(state.expiries, viewedKey)

	hub.refreshViewerKeys()

	if state.expiries[viewedKey] <= 0 {
		t.Fatal("the heartbeat must refresh the expiry of every viewer set this replica writes to")
	}
}
