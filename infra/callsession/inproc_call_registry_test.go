package callsession

import (
	"errors"
	"testing"

	callsession_domain "vozko/domain/callsession"
	"vozko/domain/conversation"
)

type stubCall struct{ id string }

func (s *stubCall) ID() string                            { return s.id }
func (s *stubCall) SendAudio([]byte) error                { return nil }
func (s *stubCall) AudioStream() <-chan []byte            { return nil }
func (s *stubCall) Events() <-chan conversation.CallEvent { return nil }
func (s *stubCall) Hangup() error                         { return nil }
func (s *stubCall) Done() <-chan struct{}                 { return nil }

func makeEntry(callID, sessionID, userID string) callsession_domain.CallEntry {
	return callsession_domain.CallEntry{
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
		e    callsession_domain.CallEntry
		want error
	}{
		{"no ws", callsession_domain.CallEntry{CallID: "c", OwnerSessionID: "s", OwnerUserID: "u"}, callsession_domain.ErrWorkspaceRequired},
		{"no call", callsession_domain.CallEntry{WorkspaceID: "w", OwnerSessionID: "s", OwnerUserID: "u"}, callsession_domain.ErrCallIDRequired},
		{"no owner", callsession_domain.CallEntry{WorkspaceID: "w", CallID: "c"}, callsession_domain.ErrOwnerRequired},
	}
	for _, c := range cases {
		if err := r.Register(c.e); !errors.Is(err, c.want) {
			t.Errorf("%s: want %v, got %v", c.name, c.want, err)
		}
	}
}
