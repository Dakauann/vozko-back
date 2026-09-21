package ws

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"testing"
	"time"

	callsession_domain "vozko/domain/callsession"
	"vozko/domain/conversation"
)

type granularAuthorizer struct {
	mu      sync.Mutex
	allowed map[string]struct{}
}

func newGranularAuthorizer(userID string, perms ...[2]string) *granularAuthorizer {
	a := &granularAuthorizer{allowed: map[string]struct{}{}}
	for _, p := range perms {
		a.allowed[userID+"|"+p[0]+"|"+p[1]] = struct{}{}
	}
	return a
}

func (a *granularAuthorizer) HasWorkspacePermission(userID, _ string, resource, action string, _ bool) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.allowed[userID+"|"+resource+"|"+action]
	return ok
}

func (granularAuthorizer) CanAccessEntry(_, _, _, _ string, _ bool) bool    { return true }
func (granularAuthorizer) CanAccessCampaign(_, _, _, _ string, _ bool) bool { return true }
func (granularAuthorizer) GetAccessibleEntryIDs(_, _ string, _ bool) []string {
	return nil
}
func (granularAuthorizer) GetDepartmentScope(_, _ string, _ bool) (conversation.DepartmentAccessScope, bool) {
	return conversation.DepartmentAccessScope{}, true
}
func (granularAuthorizer) IsWorkspaceMember(_, _ string) bool       { return true }
func (granularAuthorizer) IsWorkspaceOwnerOrAdmin(_, _ string) bool { return false }

type stubSessionRegistry struct {
	listAvailable []callsession_domain.CallSession
	listAll       []callsession_domain.CallSession
	listPresence  []callsession_domain.MemberPresence
	listBrowser   []callsession_domain.CallSession
	listener      callsession_domain.PresenceListener
}

func (r *stubSessionRegistry) ListPresence(string) []callsession_domain.MemberPresence {
	return r.listPresence
}
func (r *stubSessionRegistry) ListBrowserSessions(string) []callsession_domain.CallSession {
	return r.listBrowser
}

func (r *stubSessionRegistry) Register(callsession_domain.CallSession) (func(), error) {
	return func() {}, nil
}
func (r *stubSessionRegistry) Deregister(callsession_domain.CallSession) {}
func (r *stubSessionRegistry) FindByUser(string, string) (callsession_domain.CallSession, bool) {
	return nil, false
}
func (r *stubSessionRegistry) FindSessionsByUser(string, string) []callsession_domain.CallSession {
	return nil
}
func (r *stubSessionRegistry) FindByID(string) (callsession_domain.CallSession, bool) {
	return nil, false
}
func (r *stubSessionRegistry) ListAvailable(string) []callsession_domain.CallSession {
	return r.listAvailable
}
func (r *stubSessionRegistry) ListAll(string) []callsession_domain.CallSession {
	return r.listAll
}
func (r *stubSessionRegistry) SetPresenceListener(l callsession_domain.PresenceListener) {
	r.listener = l
}
func (r *stubSessionRegistry) NotifyPresenceChanged(string) {}

type stubCallRegistry struct{}

func (stubCallRegistry) Register(callsession_domain.CallEntry) error { return nil }
func (stubCallRegistry) Lookup(string, string) (callsession_domain.CallEntry, bool) {
	return callsession_domain.CallEntry{}, false
}
func (stubCallRegistry) Unregister(string, string) {}

func newRecordingSession(userID, workspaceID string) (*callSession, *[]*WSOutgoingMessage) {
	var mu sync.Mutex
	var out []*WSOutgoingMessage
	send := func(m *WSOutgoingMessage) {
		mu.Lock()
		defer mu.Unlock()
		out = append(out, m)
	}
	s := newCallSession("sess-"+userID, userID, workspaceID, send, nil, log.Default(), 0)
	return s, &out
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

type presenceSession struct {
	id, userID, workspaceID string
	busy                    bool
	mu                      sync.Mutex
	notified                []callsession_domain.CallSessionControlMessage
}

func (s *presenceSession) ID() string           { return s.id }
func (s *presenceSession) UserID() string       { return s.userID }
func (s *presenceSession) WorkspaceID() string  { return s.workspaceID }
func (s *presenceSession) HasActiveCall() bool  { return s.busy }
func (s *presenceSession) ActiveCallID() string { return "" }
func (s *presenceSession) Reserve(string) bool  { return !s.busy }
func (s *presenceSession) Release(string)       {}
func (s *presenceSession) Notify(msg callsession_domain.CallSessionControlMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notified = append(s.notified, msg)
	return nil
}
func (s *presenceSession) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.notified)
}
func (s *presenceSession) Shutdown(context.Context, string) error { return nil }

func TestCallSessionWS_OnPresenceChanged_CoalescesAsync(t *testing.T) {
	sess := &presenceSession{id: "s-a", userID: "user-a", workspaceID: "ws-1"}
	reg := &stubSessionRegistry{
		listBrowser:  []callsession_domain.CallSession{sess},
		listPresence: []callsession_domain.MemberPresence{{UserID: "user-a", HasBrowser: true}},
	}
	auth := newGranularAuthorizer("user-a", [2]string{"call_session", "list_members"})
	h := NewCallSessionWSHandler(nil, nil, nil, auth, log.Default(), noopWSMetricsRecorder{}).
		WithRegistries(reg, stubCallRegistry{})

	for i := 0; i < 25; i++ {
		h.OnPresenceChanged("ws-1")
	}
	if n := sess.count(); n != 0 {
		t.Fatalf("push fired synchronously (%d); OnPresenceChanged must debounce off the caller", n)
	}

	deadline := time.Now().Add(2 * time.Second)
	for sess.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := sess.count(); n != 1 {
		t.Fatalf("burst of 25 changes should coalesce to exactly 1 push, got %d", n)
	}
}

func TestCallSessionWS_OnPresenceChanged_FiltersByListMembersPerm(t *testing.T) {

	auth := newGranularAuthorizer("user-a", [2]string{"call_session", "list_members"})

	sessA := &presenceSession{id: "s-a", userID: "user-a", workspaceID: "ws-1"}
	sessB := &presenceSession{id: "s-b", userID: "user-b", workspaceID: "ws-1", busy: true}

	reg := &stubSessionRegistry{
		listBrowser: []callsession_domain.CallSession{sessA, sessB},
		listPresence: []callsession_domain.MemberPresence{
			{UserID: "user-a", HasBrowser: true},
			{UserID: "user-b", Busy: true, HasBrowser: true},
		},
	}
	h := NewCallSessionWSHandler(nil, nil, nil, auth, log.Default(), noopWSMetricsRecorder{}).
		WithRegistries(reg, stubCallRegistry{})

	h.broadcastPresence("ws-1")

	if len(sessA.notified) != 1 {
		t.Fatalf("user-a: expected 1 notify, got %d", len(sessA.notified))
	}
	if len(sessB.notified) != 1 {
		t.Fatalf("user-b: expected 1 notify, got %d", len(sessB.notified))
	}

	payloadA, ok := sessA.notified[0].Payload.(CallSessionPresencePayload)
	if !ok {
		t.Fatalf("user-a payload type: got %T", sessA.notified[0].Payload)
	}
	if len(payloadA.Users) != 2 {
		t.Fatalf("user-a: expected full roster (2 users), got %d: %+v", len(payloadA.Users), payloadA.Users)
	}

	payloadB, ok := sessB.notified[0].Payload.(CallSessionPresencePayload)
	if !ok {
		t.Fatalf("user-b payload type: got %T", sessB.notified[0].Payload)
	}
	if len(payloadB.Users) != 1 {
		t.Fatalf("user-b: expected self-only (1 user), got %d: %+v", len(payloadB.Users), payloadB.Users)
	}
	if payloadB.Users[0].UserID != "user-b" {
		t.Fatalf("user-b: self-only payload must contain user-b; got %q", payloadB.Users[0].UserID)
	}
}
