package dialer

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"vozko/domain/conversation"
	dialer_domain "vozko/domain/dialer"
)

type stubCall struct{ id string }

func (s *stubCall) ID() string                            { return s.id }
func (s *stubCall) SendAudio([]byte) error                { return nil }
func (s *stubCall) AudioStream() <-chan []byte            { return nil }
func (s *stubCall) Events() <-chan conversation.CallEvent { return nil }
func (s *stubCall) Hangup() error                         { return nil }
func (s *stubCall) Done() <-chan struct{}                 { return nil }

func makeEntry(callID, sessionID, userID string) dialer_domain.DialerCallEntry {
	return dialer_domain.DialerCallEntry{
		CallID:         callID,
		WorkspaceID:    "ws-1",
		OwnerSessionID: sessionID,
		OwnerUserID:    userID,
		Call:           &stubCall{id: callID},
	}
}

func TestInProcCallRegistry_RegisterLookupUnregister(t *testing.T) {
	r := NewInProcCallRegistry()
	if err := r.Register(makeEntry("call-1", "sess-1", "user-a")); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, ok := r.Lookup("ws-1", "call-1")
	if !ok || got.OwnerSessionID != "sess-1" || got.OwnerUserID != "user-a" {
		t.Fatalf("Lookup: %+v ok=%v", got, ok)
	}

	r.Unregister("ws-1", "call-1")
	if _, ok := r.Lookup("ws-1", "call-1"); ok {
		t.Fatal("Unregister failed")
	}
}

func TestInProcCallRegistry_RejectsInvalid(t *testing.T) {
	r := NewInProcCallRegistry()
	cases := []struct {
		name string
		e    dialer_domain.DialerCallEntry
		want error
	}{
		{"no ws", dialer_domain.DialerCallEntry{CallID: "c", OwnerSessionID: "s", OwnerUserID: "u"}, dialer_domain.ErrWorkspaceRequired},
		{"no call", dialer_domain.DialerCallEntry{WorkspaceID: "w", OwnerSessionID: "s", OwnerUserID: "u"}, dialer_domain.ErrTransferCallNotFound},
		{"no owner", dialer_domain.DialerCallEntry{WorkspaceID: "w", CallID: "c"}, dialer_domain.ErrOwnerRequired},
	}
	for _, c := range cases {
		if err := r.Register(c.e); !errors.Is(err, c.want) {
			t.Errorf("%s: want %v, got %v", c.name, c.want, err)
		}
	}
}

func TestInProcCallRegistry_Rebind_CAS(t *testing.T) {
	r := NewInProcCallRegistry()
	_ = r.Register(makeEntry("call-1", "sess-A", "user-a"))

	if err := r.Rebind("ws-1", "call-1", "sess-X", "sess-B", "user-b"); !errors.Is(err, dialer_domain.ErrTransferAlreadyInFlight) {
		t.Fatalf("wrong-expected: want ErrTransferAlreadyInFlight, got %v", err)
	}

	if err := r.Rebind("ws-1", "call-1", "sess-A", "sess-B", "user-b"); err != nil {
		t.Fatalf("Rebind: %v", err)
	}
	got, _ := r.Lookup("ws-1", "call-1")
	if got.OwnerSessionID != "sess-B" || got.OwnerUserID != "user-b" {
		t.Fatalf("after Rebind: %+v", got)
	}
}

func TestInProcCallRegistry_Rebind_UnknownCall(t *testing.T) {
	r := NewInProcCallRegistry()
	if err := r.Rebind("ws-1", "missing", "any", "new", "newU"); !errors.Is(err, dialer_domain.ErrTransferCallNotFound) {
		t.Fatalf("missing call: want ErrTransferCallNotFound, got %v", err)
	}
}

func TestInProcCallRegistry_Rebind_OnlyOneRaceWinner(t *testing.T) {
	r := NewInProcCallRegistry()
	_ = r.Register(makeEntry("call-1", "sess-A", "user-a"))

	const n = 50
	var winners atomic.Int32
	var losers atomic.Int32
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			err := r.Rebind("ws-1", "call-1", "sess-A", "sess-W-"+itoa(i), "user-w-"+itoa(i))
			switch {
			case err == nil:
				winners.Add(1)
			case errors.Is(err, dialer_domain.ErrTransferAlreadyInFlight):
				losers.Add(1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("expected exactly 1 winner, got %d (losers=%d)", winners.Load(), losers.Load())
	}
}
