package ws

import (
	"fmt"
	"sync"
	"testing"

	"vozko/domain/conversation"
)

// countingAuthorizer records how often the per-entry assignment is resolved and
// how often the plain per-user check runs, so a broadcast can be measured
// against the number of connections it serves.
type countingAuthorizer struct {
	hubDepartmentTestAuthorizer

	mu            sync.Mutex
	resolveCalls  int
	perUserChecks int
	allow         map[string]bool
}

func newCountingAuthorizer(allowedUsers ...string) *countingAuthorizer {
	allow := make(map[string]bool, len(allowedUsers))
	for _, u := range allowedUsers {
		allow[u] = true
	}
	return &countingAuthorizer{allow: allow}
}

func (c *countingAuthorizer) CanAccessEntry(userID, _, _, _ string, isAdmin bool) bool {
	c.mu.Lock()
	c.perUserChecks++
	c.mu.Unlock()
	return isAdmin || c.allow[userID]
}

func (c *countingAuthorizer) ResolveEntryAccess(_, _, _ string) func(string, bool) bool {
	c.mu.Lock()
	c.resolveCalls++
	c.mu.Unlock()
	return func(userID string, isAdmin bool) bool {
		return isAdmin || c.allow[userID]
	}
}

func (c *countingAuthorizer) counts() (resolves, perUser int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.resolveCalls, c.perUserChecks
}

var _ conversation.ConversationAuthorizer = (*countingAuthorizer)(nil)
var _ entryAccessResolver = (*countingAuthorizer)(nil)

type stubStageProvider struct{ conversation.StageProvider }

func (stubStageProvider) GetEntryStage(string, string, string) (*conversation.InboxEntryStage, error) {
	return &conversation.InboxEntryStage{}, nil
}

func hubWithConnections(auth conversation.ConversationAuthorizer, workspaceID string, n int) *ConversationHub {
	hub := NewConversationHub(auth, nil, nil, &eligibilityFakeSharedState{}, "test-replica", "")
	hub.SetWSMetrics(noopWSMetricsRecorder{})
	hub.SetStageProvider(stubStageProvider{})
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("conn-%d", i)
		user := fmt.Sprintf("user-%d", i)
		conn := &WSConnection{
			ID:          id,
			UserID:      user,
			WorkspaceID: workspaceID,
			Send:        make(chan []byte, 8),
			Done:        make(chan struct{}),
		}
		hub.connections[id] = conn
		hub.userConnections[user] = map[string]bool{id: true}
	}
	return hub
}

func TestBroadcast_ResolvesEntryAccessOncePerWorkspaceNotPerConnection(t *testing.T) {
	const connections = 40

	auth := newCountingAuthorizer()
	hub := hubWithConnections(auth, "ws-1", connections)

	hub.broadcastStageUpdateLocal("ws-1", "entry-1", "whatsapp")

	resolves, perUser := auth.counts()
	if resolves != 1 {
		t.Fatalf("expected the assignment to be resolved once for %d connections, got %d", connections, resolves)
	}
	if perUser != 0 {
		t.Fatalf("the per-connection path must not run when the authorizer can resolve once, got %d calls", perUser)
	}
}

func TestBroadcast_ResolvesOncePerDistinctWorkspace(t *testing.T) {
	auth := newCountingAuthorizer()
	hub := hubWithConnections(auth, "ws-1", 10)

	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("other-%d", i)
		user := fmt.Sprintf("other-user-%d", i)
		conn := &WSConnection{
			ID: id, UserID: user, WorkspaceID: "ws-2",
			Send: make(chan []byte, 8), Done: make(chan struct{}),
		}
		hub.connections[id] = conn
		hub.userConnections[user] = map[string]bool{id: true}
	}

	hub.broadcastStageUpdateLocal("ws-1", "entry-1", "whatsapp")

	resolves, _ := auth.counts()
	if resolves != 2 {
		t.Fatalf("two workspaces are connected, so exactly two resolutions are expected, got %d", resolves)
	}
}

func TestBroadcast_StillDeliversOnlyToPermittedConnections(t *testing.T) {
	auth := newCountingAuthorizer("user-1", "user-3")
	hub := hubWithConnections(auth, "ws-1", 5)

	hub.broadcastStageUpdateLocal("ws-1", "entry-1", "whatsapp")

	for i := 0; i < 5; i++ {
		conn := hub.connections[fmt.Sprintf("conn-%d", i)]
		delivered := len(conn.Send) > 0
		permitted := auth.allow[conn.UserID]
		if delivered != permitted {
			t.Fatalf("user %s permitted=%v but delivered=%v", conn.UserID, permitted, delivered)
		}
	}
}

func TestBroadcast_FallsBackWhenAuthorizerCannotResolve(t *testing.T) {
	plain := &hubDepartmentTestAuthorizer{entryAccess: map[string]bool{"user-0": true, "user-1": true}}
	hub := hubWithConnections(plain, "ws-1", 3)

	hub.broadcastStageUpdateLocal("ws-1", "entry-1", "whatsapp")

	for _, id := range []string{"conn-0", "conn-1"} {
		if len(hub.connections[id].Send) == 0 {
			t.Fatalf("%s should have received the broadcast through the fallback path", id)
		}
	}
	if len(hub.connections["conn-2"].Send) != 0 {
		t.Fatal("conn-2 is not permitted and must not receive the broadcast")
	}
}

// plainCountingAuthorizer deliberately does NOT implement entryAccessResolver,
// so it exercises the old cost model and documents what the resolver saves.
type plainCountingAuthorizer struct {
	hubDepartmentTestAuthorizer
	mu    sync.Mutex
	calls int
}

func (p *plainCountingAuthorizer) CanAccessEntry(_, _, _, _ string, _ bool) bool {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	return true
}

func TestBroadcast_ResolverCollapsesThePerConnectionLookups(t *testing.T) {
	const connections = 40

	plain := &plainCountingAuthorizer{}
	plainHub := hubWithConnections(plain, "ws-1", connections)
	plainHub.broadcastStageUpdateLocal("ws-1", "entry-1", "whatsapp")

	plain.mu.Lock()
	perConnection := plain.calls
	plain.mu.Unlock()

	resolving := newCountingAuthorizer()
	for i := 0; i < connections; i++ {
		resolving.allow[fmt.Sprintf("user-%d", i)] = true
	}
	resolvingHub := hubWithConnections(resolving, "ws-1", connections)
	resolvingHub.broadcastStageUpdateLocal("ws-1", "entry-1", "whatsapp")

	resolves, _ := resolving.counts()

	if perConnection != connections {
		t.Fatalf("without the resolver the broadcast should cost one lookup per connection, got %d for %d", perConnection, connections)
	}
	if resolves != 1 {
		t.Fatalf("with the resolver it should cost one lookup in total, got %d", resolves)
	}
	t.Logf("broadcast to %d connections: %d lookups before, %d after", connections, perConnection, resolves)
}
