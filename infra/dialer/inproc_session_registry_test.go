package dialer

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dialer_domain "vozko/domain/dialer"
)

type fakeSession struct {
	id, user, ws string
	active       atomic.Bool
	reserved     atomic.Value // current reservation token (string)
	callID       atomic.Value
	notifies     atomic.Int32
}

func newFakeSession(id, user, ws string) *fakeSession {
	s := &fakeSession{id: id, user: user, ws: ws}
	s.callID.Store("")
	s.reserved.Store("")
	return s
}

func (s *fakeSession) ID() string          { return s.id }
func (s *fakeSession) UserID() string      { return s.user }
func (s *fakeSession) WorkspaceID() string { return s.ws }
func (s *fakeSession) HasActiveCall() bool {
	if s.active.Load() {
		return true
	}
	tok, _ := s.reserved.Load().(string)
	return tok != ""
}
func (s *fakeSession) ActiveCallID() string {
	v, _ := s.callID.Load().(string)
	return v
}
func (s *fakeSession) Reserve(token string) bool {
	if token == "" || s.active.Load() {
		return false
	}
	tok, _ := s.reserved.Load().(string)
	if tok != "" {
		return tok == token
	}
	s.reserved.Store(token)
	return true
}
func (s *fakeSession) Release(token string) {
	tok, _ := s.reserved.Load().(string)
	if tok == token {
		s.reserved.Store("")
	}
}
func (s *fakeSession) Notify(_ dialer_domain.DialerControlMessage) error {
	s.notifies.Add(1)
	return nil
}

// fakePhysicalSession is a branch (registered SIP device): it opts into the physical
// endpoint interface so the registry treats it as a branch, not a browser.
type fakePhysicalSession struct{ *fakeSession }

func (fakePhysicalSession) IsPhysicalEndpoint() bool { return true }

func TestInProcSessionRegistry_ListPresence(t *testing.T) {
	r := NewInProcSessionRegistry()

	// user-a: browser + branch, both free -> available, both kinds.
	browser := newFakeSession("s-a-web", "user-a", "ws-1")
	branch := fakePhysicalSession{newFakeSession("s-a-branch", "user-a", "ws-1")}
	// user-b: browser only, on a call -> busy.
	busyWeb := newFakeSession("s-b-web", "user-b", "ws-1")
	busyWeb.active.Store(true)
	// user-c: browser idle but the branch is on a call -> busy. A person on a call on
	// ANY endpoint is busy; an idle browser must not mask an in-progress branch call.
	cWeb := newFakeSession("s-c-web", "user-c", "ws-1")
	cBranch := fakePhysicalSession{newFakeSession("s-c-branch", "user-c", "ws-1")}
	cBranch.active.Store(true)

	for _, s := range []dialer_domain.DialerSession{browser, branch, busyWeb, cWeb, cBranch} {
		if _, err := r.Register(s); err != nil {
			t.Fatal(err)
		}
	}

	byUser := map[string]dialer_domain.MemberPresence{}
	for _, p := range r.ListPresence("ws-1") {
		byUser[p.UserID] = p
	}
	if a := byUser["user-a"]; !a.HasBrowser || !a.HasBranch || a.Busy {
		t.Fatalf("user-a = %+v, want browser+branch, available", a)
	}
	if b := byUser["user-b"]; !b.HasBrowser || b.HasBranch || !b.Busy {
		t.Fatalf("user-b = %+v, want browser-only, busy", b)
	}
	if c := byUser["user-c"]; !c.HasBrowser || !c.HasBranch || !c.Busy {
		t.Fatalf("user-c = %+v, want busy: on a call via the branch even with an idle browser", c)
	}

	// ListBrowserSessions is the push target: browsers only, never the branch (a SIP
	// phone has no WebSocket to receive a presence snapshot).
	browsers := r.ListBrowserSessions("ws-1")
	for _, s := range browsers {
		if strings.Contains(s.ID(), "branch") {
			t.Fatalf("ListBrowserSessions leaked a branch: %s", s.ID())
		}
	}
	if len(browsers) != 3 { // s-a-web, s-b-web, s-c-web
		t.Fatalf("ListBrowserSessions = %d, want 3 browsers", len(browsers))
	}
}

func TestInProcSessionRegistry_RegisterAndFind(t *testing.T) {
	r := NewInProcSessionRegistry()
	s := newFakeSession("sess-1", "user-a", "ws-1")

	dereg, err := r.Register(s)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if dereg == nil {
		t.Fatal("Register must return a deregister closure")
	}

	got, ok := r.FindByUser("ws-1", "user-a")
	if !ok || got.ID() != "sess-1" {
		t.Fatalf("FindByUser: got=%v ok=%v", got, ok)
	}

	dereg()
	if _, ok := r.FindByUser("ws-1", "user-a"); ok {
		t.Fatal("deregister did not remove session")
	}

	dereg()
}

func TestInProcSessionRegistry_LastWriteWins(t *testing.T) {
	r := NewInProcSessionRegistry()
	first := newFakeSession("sess-1", "user-a", "ws-1")
	second := newFakeSession("sess-2", "user-a", "ws-1")

	dereg1, _ := r.Register(first)
	_, _ = r.Register(second)

	got, _ := r.FindByUser("ws-1", "user-a")
	if got.ID() != "sess-2" {
		t.Fatalf("expected sess-2, got %s", got.ID())
	}

	dereg1()
	got, ok := r.FindByUser("ws-1", "user-a")
	if !ok || got.ID() != "sess-2" {
		t.Fatalf("old deregister evicted current session: got=%v ok=%v", got, ok)
	}
}

func TestInProcSessionRegistry_ListAvailable(t *testing.T) {
	r := NewInProcSessionRegistry()
	a := newFakeSession("sa", "ua", "ws-1")
	b := newFakeSession("sb", "ub", "ws-1")
	c := newFakeSession("sc", "uc", "ws-2")
	_, _ = r.Register(a)
	_, _ = r.Register(b)
	_, _ = r.Register(c)
	b.active.Store(true)

	avail := r.ListAvailable("ws-1")
	if len(avail) != 1 || avail[0].ID() != "sa" {
		t.Fatalf("ListAvailable(ws-1) = %v", avail)
	}
}

func TestInProcSessionRegistry_ListAvailableExcludesReserved(t *testing.T) {
	r := NewInProcSessionRegistry()
	a := newFakeSession("sa", "ua", "ws-1")
	b := newFakeSession("sb", "ub", "ws-1")
	_, _ = r.Register(a)
	_, _ = r.Register(b)

	// A reserved (ringing) agent has no attached call yet, but must drop out of the
	// availability pool so no concurrent flow rings it a second time.
	if !a.Reserve("offer-1") {
		t.Fatal("Reserve should succeed on an idle session")
	}
	avail := r.ListAvailable("ws-1")
	if len(avail) != 1 || avail[0].ID() != "sb" {
		got := make([]string, len(avail))
		for i, s := range avail {
			got[i] = s.ID()
		}
		t.Fatalf("ListAvailable must exclude the reserved session; got %v", got)
	}

	// Releasing returns it to the pool.
	a.Release("offer-1")
	if got := len(r.ListAvailable("ws-1")); got != 2 {
		t.Fatalf("after release ListAvailable = %d, want 2", got)
	}
}

func TestInProcSessionRegistry_ListAvailableOneHumanOneCall(t *testing.T) {
	// A member reachable on two endpoints (browser + branch) who is on a call on ONE
	// of them is busy: an idle browser must not make them offerable while the branch
	// is in a call. "One human = one call".
	r := NewInProcSessionRegistry()
	web := newFakeSession("m-web", "user-m", "ws-1")
	branch := fakePhysicalSession{newFakeSession("m-branch", "user-m", "ws-1")}
	branch.active.Store(true) // on a call via the branch; the browser is idle
	_, _ = r.Register(web)
	_, _ = r.Register(branch)

	if avail := r.ListAvailable("ws-1"); len(avail) != 0 {
		got := make([]string, len(avail))
		for i, s := range avail {
			got[i] = s.ID()
		}
		t.Fatalf("member busy on the branch must be excluded from ListAvailable even with a free browser; got %v", got)
	}
	// They still appear in ListAll, represented by the busy branch.
	all := r.ListAll("ws-1")
	if len(all) != 1 || all[0].ID() != "m-branch" {
		t.Fatalf("ListAll should surface the member via the busy branch; got %+v", all)
	}
}

func TestInProcSessionRegistry_ListAvailablePreservesConnectionOrder(t *testing.T) {
	r := NewInProcSessionRegistry()
	a := newFakeSession("sa", "ua", "ws-1")
	b := newFakeSession("sb", "ub", "ws-1")
	c := newFakeSession("sc", "uc", "ws-1")
	for _, s := range []*fakeSession{a, b, c} {
		if _, err := r.Register(s); err != nil {
			t.Fatalf("Register(%s): %v", s.ID(), err)
		}
	}

	avail := r.ListAvailable("ws-1")
	ids := []string{avail[0].ID(), avail[1].ID(), avail[2].ID()}
	if got, want := strings.Join(ids, ","), "sa,sb,sc"; got != want {
		t.Fatalf("ListAvailable order = %s, want %s", got, want)
	}
}

func TestInProcSessionRegistry_ReregisterMovesUserToEnd(t *testing.T) {
	r := NewInProcSessionRegistry()
	a := newFakeSession("sa", "ua", "ws-1")
	b := newFakeSession("sb", "ub", "ws-1")
	c := newFakeSession("sc", "uc", "ws-1")
	if _, err := r.Register(a); err != nil {
		t.Fatalf("Register(a): %v", err)
	}
	if _, err := r.Register(b); err != nil {
		t.Fatalf("Register(b): %v", err)
	}
	if _, err := r.Register(c); err != nil {
		t.Fatalf("Register(c): %v", err)
	}

	a2 := newFakeSession("sa2", "ua", "ws-1")
	if _, err := r.Register(a2); err != nil {
		t.Fatalf("Register(a2): %v", err)
	}

	avail := r.ListAvailable("ws-1")
	ids := []string{avail[0].ID(), avail[1].ID(), avail[2].ID()}
	if got, want := strings.Join(ids, ","), "sb,sc,sa2"; got != want {
		t.Fatalf("ListAvailable after re-register = %s, want %s", got, want)
	}
}

func TestInProcSessionRegistry_RejectsInvalid(t *testing.T) {
	r := NewInProcSessionRegistry()
	if _, err := r.Register(nil); !errors.Is(err, dialer_domain.ErrSessionNotFound) {
		t.Errorf("nil session: want ErrSessionNotFound, got %v", err)
	}
	if _, err := r.Register(newFakeSession("sid", "uid", "")); !errors.Is(err, dialer_domain.ErrWorkspaceRequired) {
		t.Errorf("empty ws: want ErrWorkspaceRequired, got %v", err)
	}
	if _, err := r.Register(newFakeSession("sid", "", "ws")); !errors.Is(err, dialer_domain.ErrOwnerRequired) {
		t.Errorf("empty user: want ErrOwnerRequired, got %v", err)
	}
}

func TestInProcSessionRegistry_ConcurrentRegisterDeregister(t *testing.T) {
	r := NewInProcSessionRegistry()
	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			s := newFakeSession(
				"sess-"+itoa(i),
				"user-"+itoa(i),
				"ws-1",
			)
			dereg, err := r.Register(s)
			if err != nil {
				t.Errorf("Register: %v", err)
				return
			}
			_, _ = r.FindByUser("ws-1", s.user)
			dereg()
		}()
	}
	wg.Wait()
	if got := len(r.ListAvailable("ws-1")); got != 0 {
		t.Fatalf("expected workspace empty after deregister, got %d sessions", got)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}

type fakeListener struct {
	calls atomic.Int32
	mu    sync.Mutex
	seen  []string
}

func (f *fakeListener) OnPresenceChanged(workspaceID string) {
	f.calls.Add(1)
	f.mu.Lock()
	f.seen = append(f.seen, workspaceID)
	f.mu.Unlock()
}

func (f *fakeListener) workspaces() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.seen))
	copy(out, f.seen)
	return out
}

func TestInProcSessionRegistry_ListAll(t *testing.T) {
	r := NewInProcSessionRegistry()
	a := newFakeSession("s-a", "user-a", "ws-1")
	b := newFakeSession("s-b", "user-b", "ws-1")
	c := newFakeSession("s-c", "user-c", "ws-2")
	for _, s := range []*fakeSession{a, b, c} {
		if _, err := r.Register(s); err != nil {
			t.Fatalf("register %s: %v", s.ID(), err)
		}
	}

	b.active.Store(true)

	all := r.ListAll("ws-1")
	if len(all) != 2 {
		t.Fatalf("ListAll(ws-1): want 2, got %d", len(all))
	}
	if got := len(r.ListAvailable("ws-1")); got != 1 {
		t.Fatalf("ListAvailable(ws-1) sanity: want 1, got %d", got)
	}
	if got := len(r.ListAll("ws-2")); got != 1 {
		t.Fatalf("ListAll(ws-2): want 1, got %d", got)
	}
	if got := len(r.ListAll("ws-missing")); got != 0 {
		t.Fatalf("ListAll(missing): want 0, got %d", got)
	}
}

func TestInProcSessionRegistry_PresenceListener_FiresOnRegisterAndDeregister(t *testing.T) {
	r := NewInProcSessionRegistry()
	l := &fakeListener{}
	r.SetPresenceListener(l)

	s := newFakeSession("sess-1", "user-a", "ws-1")
	dereg, err := r.Register(s)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if got := l.calls.Load(); got != 1 {
		t.Fatalf("after register: want 1 call, got %d", got)
	}

	dereg()
	if got := l.calls.Load(); got != 2 {
		t.Fatalf("after deregister: want 2 calls, got %d", got)
	}
	seen := l.workspaces()
	if len(seen) != 2 || seen[0] != "ws-1" || seen[1] != "ws-1" {
		t.Fatalf("unexpected workspace fan-out: %v", seen)
	}
}

func TestInProcSessionRegistry_NotifyPresenceChanged_FiresListener(t *testing.T) {
	r := NewInProcSessionRegistry()
	l := &fakeListener{}
	r.SetPresenceListener(l)

	r.NotifyPresenceChanged("ws-7")
	if got := l.calls.Load(); got != 1 {
		t.Fatalf("want 1 call, got %d", got)
	}
	if seen := l.workspaces(); len(seen) != 1 || seen[0] != "ws-7" {
		t.Fatalf("unexpected workspaces: %v", seen)
	}

	r.NotifyPresenceChanged("")
	if got := l.calls.Load(); got != 1 {
		t.Fatalf("empty ws should be no-op, got %d", got)
	}
}

func TestInProcSessionRegistry_SetPresenceListener_NilClears(t *testing.T) {
	r := NewInProcSessionRegistry()
	l := &fakeListener{}
	r.SetPresenceListener(l)

	if _, err := r.Register(newFakeSession("s-1", "u-1", "ws-1")); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if got := l.calls.Load(); got != 1 {
		t.Fatalf("want 1 call before clear, got %d", got)
	}

	r.SetPresenceListener(nil)
	r.NotifyPresenceChanged("ws-1")
	if _, err := r.Register(newFakeSession("s-2", "u-2", "ws-1")); err != nil {
		t.Fatalf("Register after clear: %v", err)
	}
	if got := l.calls.Load(); got != 1 {
		t.Fatalf("listener still firing after nil clear: got %d", got)
	}
}

func TestInProcSessionRegistry_NoTimeDriftInDeregister(_ *testing.T) {

	r := NewInProcSessionRegistry()
	dereg, _ := r.Register(newFakeSession("s", "u", "w"))
	done := make(chan struct{})
	go func() { dereg(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		panic("deregister blocked")
	}
}
