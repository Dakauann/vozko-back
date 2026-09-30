package ws

import (
	"context"
	"log"
	"sync"
	"testing"
	"time"

	"vozko/domain/callrouting"
	"vozko/domain/conversation"
	"vozko/domain/voip"
	callsession_usecase "vozko/usecases/callsession"
)

type activityLog struct {
	mu       sync.Mutex
	answered []string
	ended    []string
}

func (a *activityLog) Stats(string, []string) []callrouting.AgentStats { return nil }

func (a *activityLog) CallEnded(_ string, userID string, _ time.Time) {
	a.mu.Lock()
	a.ended = append(a.ended, userID)
	a.mu.Unlock()
}

func (a *activityLog) CallAnswered(_ string, userID string) {
	a.mu.Lock()
	a.answered = append(a.answered, userID)
	a.mu.Unlock()
}

func (a *activityLog) snapshot() ([]string, []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.answered...), append([]string(nil), a.ended...)
}

type messageLog struct {
	mu  sync.Mutex
	out []*WSOutgoingMessage
}

func (m *messageLog) send(msg *WSOutgoingMessage) {
	m.mu.Lock()
	m.out = append(m.out, msg)
	m.mu.Unlock()
}

func (m *messageLog) types() []WSEventType {
	m.mu.Lock()
	defer m.mu.Unlock()
	types := make([]WSEventType, 0, len(m.out))
	for _, msg := range m.out {
		types = append(types, msg.Type)
	}
	return types
}

func operatorSession(userID string, activity callrouting.AgentActivity) (*callSession, *messageLog) {
	messages := &messageLog{}
	s := newCallSession("sess-"+userID, userID, "ws-1", messages.send, nil, log.Default(), 0)
	s.SetActivity(activity)
	return s, messages
}

func newChannels(t *testing.T, activity callrouting.AgentActivity) *CallChannels {
	t.Helper()
	lifecycle, err := callsession_usecase.NewOutboundCallLifecycleRunner(nil, nil, nil, noopBillingPub{}, log.Default())
	if err != nil {
		t.Fatalf("lifecycle: %v", err)
	}
	return NewCallChannels(activity, lifecycle, &fakeEndUseCase{}, nil, log.Default())
}

func eventually(t *testing.T, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestAParkedCallIsFindableUntilItEnds(t *testing.T) {
	channels := newChannels(t, &activityLog{})
	call := newFakeCallSessionCRMCall("sip-in-1")

	routed, err := channels.Park(context.Background(), callrouting.ParkInput{Call: call, WorkspaceID: "ws-1", Phone: "5584994409684"})
	if err != nil {
		t.Fatalf("Park: %v", err)
	}
	found, ok := channels.Find("ws-1", "sip-in-1")
	if !ok || found.RemoteNumber() != "5584994409684" {
		t.Fatalf("parked call not found: %v", ok)
	}
	if _, owned := routed.OwnerSession(); owned {
		t.Fatal("a parked call has no operator")
	}
	if _, ok := channels.Find("ws-other", "sip-in-1"); ok {
		t.Fatal("another workspace found the call")
	}

	if err := routed.SendAudio([]byte{1, 2}); err != nil {
		t.Fatalf("SendAudio: %v", err)
	}
	eventually(t, "audio to reach the caller", func() bool { return len(call.snapshotAudioCalls()) == 1 })

	call.closeDoneOnce()
	eventually(t, "the ended call to leave the directory", func() bool {
		_, ok := channels.Find("ws-1", "sip-in-1")
		return !ok
	})
}

func TestHoldReleasesTheOperatorAndConnectHandsTheCallerToAnother(t *testing.T) {
	activity := &activityLog{}
	channels := newChannels(t, activity)
	call := newFakeCallSessionCRMCall("sip-in-2")
	routed, _ := channels.Park(context.Background(), callrouting.ParkInput{Call: call, WorkspaceID: "ws-1"})
	ana, anaMessages := operatorSession("ana", activity)
	bia, biaMessages := operatorSession("bia", activity)

	if err := routed.Connect(ana); err != nil {
		t.Fatalf("Connect ana: %v", err)
	}
	if owner, _ := routed.OwnerSession(); owner.UserID() != "ana" {
		t.Fatal("ana does not own the call")
	}

	music := make([]byte, 3*voip.PCMFrameBytes)
	if err := routed.Hold(music); err != nil {
		t.Fatalf("Hold: %v", err)
	}
	if ana.Current() != nil {
		t.Fatal("ana still holds the call after it went on hold")
	}
	eventually(t, "hold music to play", func() bool { return len(call.snapshotAudioCalls()) >= 3 })

	if err := routed.Connect(bia); err != nil {
		t.Fatalf("Connect bia: %v", err)
	}
	played := len(call.snapshotAudioCalls())
	time.Sleep(4 * voip.PCMFrameDuration)
	if len(call.snapshotAudioCalls()) != played {
		t.Fatal("hold music kept playing after an operator took the call")
	}
	if bia.Current() == nil {
		t.Fatal("bia did not get the call")
	}
	for _, msgType := range anaMessages.types() {
		if msgType == WSEventCallEnded {
			t.Fatal("ana was told the call ended when it was only moved")
		}
	}
	if got := biaMessages.types(); len(got) == 0 || got[0] != WSEventCallStatus {
		t.Fatalf("bia messages = %v, want the call status first", got)
	}

	call.events <- conversation.CallEvent{Type: conversation.CallEventEnded}
	eventually(t, "the call to end", func() bool {
		_, ended := activity.snapshot()
		return len(ended) == 1
	})
	answered, ended := activity.snapshot()
	if len(answered) != 2 || answered[1] != "bia" || ended[0] != "bia" {
		t.Fatalf("activity answered=%v ended=%v", answered, ended)
	}
}

func TestACallCannotConnectToABusyOperator(t *testing.T) {
	channels := newChannels(t, nil)
	first, _ := channels.Park(context.Background(), callrouting.ParkInput{Call: newFakeCallSessionCRMCall("c1"), WorkspaceID: "ws-1"})
	second, _ := channels.Park(context.Background(), callrouting.ParkInput{Call: newFakeCallSessionCRMCall("c2"), WorkspaceID: "ws-1"})
	ana, _ := operatorSession("ana", nil)

	if err := first.Connect(ana); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := second.Connect(ana); err == nil {
		t.Fatal("an operator on a call received a second caller")
	}
}
