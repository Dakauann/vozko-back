package ws

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"vozko/domain/conversation"
	dialer_domain "vozko/domain/dialer"
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
	listAvailable []dialer_domain.DialerSession
	listAll       []dialer_domain.DialerSession
	listPresence  []dialer_domain.MemberPresence
	listBrowser   []dialer_domain.DialerSession
	listener      dialer_domain.PresenceListener
}

func (r *stubSessionRegistry) ListPresence(string) []dialer_domain.MemberPresence {
	return r.listPresence
}
func (r *stubSessionRegistry) ListBrowserSessions(string) []dialer_domain.DialerSession {
	return r.listBrowser
}

func (r *stubSessionRegistry) Register(dialer_domain.DialerSession) (func(), error) {
	return func() {}, nil
}
func (r *stubSessionRegistry) Deregister(dialer_domain.DialerSession) {}
func (r *stubSessionRegistry) FindByUser(string, string) (dialer_domain.DialerSession, bool) {
	return nil, false
}
func (r *stubSessionRegistry) FindSessionsByUser(string, string) []dialer_domain.DialerSession {
	return nil
}
func (r *stubSessionRegistry) FindByID(string) (dialer_domain.DialerSession, bool) {
	return nil, false
}
func (r *stubSessionRegistry) ListAvailable(string) []dialer_domain.DialerSession {
	return r.listAvailable
}
func (r *stubSessionRegistry) ListAll(string) []dialer_domain.DialerSession {
	return r.listAll
}
func (r *stubSessionRegistry) SetPresenceListener(l dialer_domain.PresenceListener) {
	r.listener = l
}
func (r *stubSessionRegistry) NotifyPresenceChanged(string) {}

type stubCallRegistry struct{}

func (stubCallRegistry) Register(dialer_domain.DialerCallEntry) error { return nil }
func (stubCallRegistry) Lookup(string, string) (dialer_domain.DialerCallEntry, bool) {
	return dialer_domain.DialerCallEntry{}, false
}
func (stubCallRegistry) Rebind(string, string, string, string, string) error { return nil }
func (stubCallRegistry) Unregister(string, string)                           {}

type stubTransferUseCase struct {
	initiateCalls int32
	acceptCalls   int32
}

func (s *stubTransferUseCase) Initiate(context.Context, dialer_domain.TransferRequest) (*dialer_domain.TransferHandle, error) {
	s.initiateCalls++
	return &dialer_domain.TransferHandle{ID: "tid-1", CallID: "call-1", TargetUserID: "target", Kind: dialer_domain.TransferKindBlind, Stage: dialer_domain.TransferStagePendingOffer}, nil
}
func (s *stubTransferUseCase) Accept(context.Context, string, string, string) (*dialer_domain.TransferHandle, error) {
	s.acceptCalls++
	return &dialer_domain.TransferHandle{}, nil
}
func (s *stubTransferUseCase) Decline(context.Context, string, string, string) error { return nil }
func (s *stubTransferUseCase) CompleteAttended(context.Context, string, string) (*dialer_domain.TransferHandle, error) {
	return &dialer_domain.TransferHandle{}, nil
}
func (s *stubTransferUseCase) CancelAttended(context.Context, string, string, string) error {
	return nil
}
func (s *stubTransferUseCase) AbortByDisconnect(context.Context, string, string) error   { return nil }
func (s *stubTransferUseCase) AbortByCall(context.Context, string, string, string) error { return nil }
func (s *stubTransferUseCase) AbortByCallLegDeath(context.Context, string, string) error { return nil }
func (s *stubTransferUseCase) FindActiveByCall(string, string) (*dialer_domain.TransferHandle, bool) {
	return nil, false
}
func (s *stubTransferUseCase) ReapExpiredOffers(context.Context, time.Time) int { return 0 }
func (s *stubTransferUseCase) Tick(context.Context, time.Time) int              { return 0 }

func newRecordingSession(userID, workspaceID string) (*dialerSession, *[]*WSOutgoingMessage) {
	var mu sync.Mutex
	var out []*WSOutgoingMessage
	send := func(m *WSOutgoingMessage) {
		mu.Lock()
		defer mu.Unlock()
		out = append(out, m)
	}
	s := newDialerSession("sess-"+userID, userID, workspaceID, send, nil, log.Default(), 0)
	return s, &out
}

func buildHandlerWithTransfer(t *testing.T, auth *granularAuthorizer) (*DialerWSHandler, *stubSessionRegistry, *stubTransferUseCase) {
	t.Helper()
	reg := &stubSessionRegistry{}
	uc := &stubTransferUseCase{}
	h := NewDialerWSHandler(nil, nil, nil, auth, log.Default(), noopWSMetricsRecorder{}).
		WithTransfer(reg, stubCallRegistry{}, uc)
	return h, reg, uc
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func findError(msgs []*WSOutgoingMessage) *TransferErrorPayload {
	for _, m := range msgs {
		if m == nil || m.Type != WSEventTransferError {
			continue
		}
		if p, ok := m.Payload.(TransferErrorPayload); ok {
			return &p
		}
	}
	return nil
}

func TestDialerWS_TransferInitiate_RejectsWithoutTransferPerm(t *testing.T) {
	auth := newGranularAuthorizer("user-a", [2]string{"dialer", "use"})
	h, _, uc := buildHandlerWithTransfer(t, auth)
	session, sent := newRecordingSession("user-a", "ws-1")

	h.handleTransferInitiate(session, mustMarshal(t, TransferInitiatePayload{
		CallID: "call-1", TargetUserID: "user-b", Kind: "blind",
	}))

	got := findError(*sent)
	if got == nil {
		t.Fatalf("expected transfer:error, got: %+v", *sent)
	}
	if got.Code != "unauthorized" {
		t.Fatalf("expected code=unauthorized, got %q (msg=%q)", got.Code, got.Message)
	}
	if uc.initiateCalls != 0 {
		t.Fatalf("use case must NOT be reached when unauthorized; got %d Initiate calls", uc.initiateCalls)
	}
}

func TestDialerWS_TransferInitiate_AllowedWithTransferPermOnly(t *testing.T) {
	auth := newGranularAuthorizer("user-a", [2]string{"dialer", "transfer"})
	h, _, uc := buildHandlerWithTransfer(t, auth)
	session, sent := newRecordingSession("user-a", "ws-1")

	h.handleTransferInitiate(session, mustMarshal(t, TransferInitiatePayload{
		CallID: "call-1", TargetUserID: "user-b", Kind: "blind",
	}))

	if got := findError(*sent); got != nil && got.Code == "unauthorized" {
		t.Fatalf("expected initiate to proceed past auth; got unauthorized: %+v", got)
	}
	if uc.initiateCalls != 1 {
		t.Fatalf("expected use case to be invoked once; got %d", uc.initiateCalls)
	}
}

func TestDialerWS_TransferComplete_RequiresTransferPerm(t *testing.T) {
	auth := newGranularAuthorizer("user-a", [2]string{"dialer", "use"})
	h, _, _ := buildHandlerWithTransfer(t, auth)
	session, sent := newRecordingSession("user-a", "ws-1")

	h.handleTransferAction(session, mustMarshal(t, TransferActionPayload{TransferID: "tid-1"}), transferActionComplete)

	got := findError(*sent)
	if got == nil || got.Code != "unauthorized" {
		t.Fatalf("expected unauthorized error, got %+v", got)
	}
}

func TestDialerWS_TransferAccept_OnlyRequiresDialerUse(t *testing.T) {
	auth := newGranularAuthorizer("user-a", [2]string{"dialer", "use"})
	h, _, uc := buildHandlerWithTransfer(t, auth)
	session, sent := newRecordingSession("user-a", "ws-1")

	h.handleTransferAction(session, mustMarshal(t, TransferActionPayload{TransferID: "tid-1"}), transferActionAccept)

	if got := findError(*sent); got != nil && got.Code == "unauthorized" {
		t.Fatalf("accept must succeed with dialer:use; got unauthorized: %+v", got)
	}
	if uc.acceptCalls != 1 {
		t.Fatalf("expected Accept to be called once, got %d", uc.acceptCalls)
	}
}

func TestDialerWS_ListTargets_RequiresListMembersPerm(t *testing.T) {
	auth := newGranularAuthorizer("user-a",
		[2]string{"dialer", "use"},
		[2]string{"dialer", "transfer"},
	)
	h, _, _ := buildHandlerWithTransfer(t, auth)
	session, sent := newRecordingSession("user-a", "ws-1")

	h.handleTransferListTargets(session)

	got := findError(*sent)
	if got == nil || got.Code != "unauthorized" {
		t.Fatalf("expected unauthorized error, got %+v", got)
	}
	if !strings.Contains(strings.ToLower(got.Message), "list") {
		t.Fatalf("expected message to mention listing; got %q", got.Message)
	}
}

func TestDialerWS_ListTargets_AllowedWithListMembersPerm(t *testing.T) {
	auth := newGranularAuthorizer("user-a", [2]string{"dialer", "list_members"})
	h, _, _ := buildHandlerWithTransfer(t, auth)
	session, sent := newRecordingSession("user-a", "ws-1")

	h.handleTransferListTargets(session)

	if got := findError(*sent); got != nil {
		t.Fatalf("expected no transfer:error, got %+v", got)
	}
	if len(*sent) != 1 || (*sent)[0].Type != WSEventTransferTargets {
		t.Fatalf("expected single transfer:targets frame, got %+v", *sent)
	}
}

type presenceSession struct {
	id, userID, workspaceID string
	busy                    bool
	mu                      sync.Mutex
	notified                []dialer_domain.DialerControlMessage
}

func (s *presenceSession) ID() string           { return s.id }
func (s *presenceSession) UserID() string       { return s.userID }
func (s *presenceSession) WorkspaceID() string  { return s.workspaceID }
func (s *presenceSession) HasActiveCall() bool  { return s.busy }
func (s *presenceSession) ActiveCallID() string { return "" }
func (s *presenceSession) Reserve(string) bool  { return !s.busy }
func (s *presenceSession) Release(string)       {}
func (s *presenceSession) Notify(msg dialer_domain.DialerControlMessage) error {
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

// A burst of presence changes must be debounced into ONE async push, and it must NOT
// block the caller (the transfer hot path calls OnPresenceChanged inline).
func TestDialerWS_OnPresenceChanged_CoalescesAsync(t *testing.T) {
	sess := &presenceSession{id: "s-a", userID: "user-a", workspaceID: "ws-1"}
	reg := &stubSessionRegistry{
		listBrowser:  []dialer_domain.DialerSession{sess},
		listPresence: []dialer_domain.MemberPresence{{UserID: "user-a", HasBrowser: true}},
	}
	auth := newGranularAuthorizer("user-a", [2]string{"dialer", "list_members"})
	h := NewDialerWSHandler(nil, nil, nil, auth, log.Default(), noopWSMetricsRecorder{}).
		WithTransfer(reg, stubCallRegistry{}, &stubTransferUseCase{})

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

func TestDialerWS_OnPresenceChanged_FiltersByListMembersPerm(t *testing.T) {

	auth := newGranularAuthorizer("user-a", [2]string{"dialer", "list_members"})

	sessA := &presenceSession{id: "s-a", userID: "user-a", workspaceID: "ws-1"}
	sessB := &presenceSession{id: "s-b", userID: "user-b", workspaceID: "ws-1", busy: true}

	reg := &stubSessionRegistry{
		listBrowser: []dialer_domain.DialerSession{sessA, sessB}, // who receives the push
		listPresence: []dialer_domain.MemberPresence{
			{UserID: "user-a", HasBrowser: true},
			{UserID: "user-b", Busy: true, HasBrowser: true},
		},
	}
	h := NewDialerWSHandler(nil, nil, nil, auth, log.Default(), noopWSMetricsRecorder{}).
		WithTransfer(reg, stubCallRegistry{}, &stubTransferUseCase{})

	h.broadcastPresence("ws-1") // the synchronous build+fan-out (OnPresenceChanged just debounces it)

	if len(sessA.notified) != 1 {
		t.Fatalf("user-a: expected 1 notify, got %d", len(sessA.notified))
	}
	if len(sessB.notified) != 1 {
		t.Fatalf("user-b: expected 1 notify, got %d", len(sessB.notified))
	}

	payloadA, ok := sessA.notified[0].Payload.(DialerPresencePayload)
	if !ok {
		t.Fatalf("user-a payload type: got %T", sessA.notified[0].Payload)
	}
	if len(payloadA.Users) != 2 {
		t.Fatalf("user-a: expected full roster (2 users), got %d: %+v", len(payloadA.Users), payloadA.Users)
	}

	payloadB, ok := sessB.notified[0].Payload.(DialerPresencePayload)
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
