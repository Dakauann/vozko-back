package sip_trunk_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/sip_trunk"
)

type grantedCallers map[string]bool

func (g grantedCallers) MayCallThroughTrunks(userID, workspaceID string, isAdmin bool) bool {
	return isAdmin || g[userID+"|"+workspaceID]
}

const caller = "user-1"

func dialInput(number string) conversation.CallDialInput {
	return conversation.CallDialInput{
		PhoneNumber: number,
		UserID:      caller,
		WorkspaceID: ownerWorkspace,
		TrunkID:     "trunk-owned",
	}
}

func newSource(trunk *sip_trunk.SIPTrunk) (*CallSource, *fakeEngine) {
	engine := newFakeEngine()
	engine.markRegistered(trunk.ID)
	source := NewCallSource(newPlanner(engine, trunk), engine)
	return source, engine
}

func nextEvent(t *testing.T, call conversation.CRMCall) conversation.CallEvent {
	t.Helper()
	select {
	case ev, ok := <-call.Events():
		if !ok {
			t.Fatal("events closed before the expected event")
		}
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("no call event")
	}
	return conversation.CallEvent{}
}

func waitDone(t *testing.T, call conversation.CRMCall) {
	t.Helper()
	select {
	case <-call.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("call never finished")
	}
}

func TestDialRefusesCallsTheCallerMayNotPlace(t *testing.T) {
	disabled := ownedTrunk()
	disabled.Enabled = false
	inboundOnly := ownedTrunk()
	inboundOnly.TrunkType = sip_trunk.TrunkTypeInbound
	cases := []struct {
		name  string
		trunk *sip_trunk.SIPTrunk
		input conversation.CallDialInput
		setup func(*fakeEngine)
		want  error
	}{
		{"caller without the call permission", ownedTrunk(), func() conversation.CallDialInput { in := dialInput("100"); in.UserID = "someone-else"; return in }(), nil, sip_trunk.ErrCallNotPermitted},
		{"trunk of another workspace", ownedTrunk(), func() conversation.CallDialInput {
			in := dialInput("100")
			in.WorkspaceID = strangerWorkspace
			return in
		}(), nil, sip_trunk.ErrCallNotPermitted},
		{"unknown trunk", ownedTrunk(), func() conversation.CallDialInput { in := dialInput("100"); in.TrunkID = "missing"; return in }(), nil, sip_trunk.ErrTrunkNotFound},
		{"disabled trunk", disabled, dialInput("100"), nil, sip_trunk.ErrTrunkDisabled},
		{"inbound-only trunk", inboundOnly, dialInput("100"), nil, sip_trunk.ErrTrunkCannotDial},
		{"trunk not registered", ownedTrunk(), dialInput("100"), func(e *fakeEngine) { delete(e.statuses, "trunk-owned") }, sip_trunk.ErrTrunkNotRegistered},
		{"number with SIP syntax", ownedTrunk(), dialInput("100@evil.example"), nil, sip_trunk.ErrInvalidPhoneNumber},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source, engine := newSource(tc.trunk)
			if tc.setup != nil {
				tc.setup(engine)
			}
			call, err := source.Dial(context.Background(), tc.input)
			if !errors.Is(err, tc.want) || call != nil {
				t.Fatalf("Dial() = %v, %v, want %v and no call", call, err, tc.want)
			}
			if engine.inviteCount() != 0 {
				t.Fatal("the engine was asked to dial")
			}
		})
	}
}

func TestDialAllowsSystemAdmins(t *testing.T) {
	source, _ := newSource(ownedTrunk())
	in := dialInput("100")
	in.UserID, in.IsAdmin = "support", true
	call, err := source.Dial(context.Background(), in)
	if err != nil {
		t.Fatalf("Dial() as system admin = %v", err)
	}
	_ = call.Hangup()
	waitDone(t, call)
}

func TestAnsweredCallBridgesAudioBothWaysUntilTheRemoteHangsUp(t *testing.T) {
	source, engine := newSource(ownedTrunk())
	call, err := source.Dial(context.Background(), dialInput("+55 (11) 99999-0000"))
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	if !strings.HasPrefix(call.ID(), "sip-out-") {
		t.Fatalf("call id %q, want a sip-out- id", call.ID())
	}
	if ev := nextEvent(t, call); ev.Type != conversation.CallEventRinging {
		t.Fatalf("first event = %s, want ringing", ev.Type)
	}
	if ev := nextEvent(t, call); ev.Type != conversation.CallEventAnswered {
		t.Fatalf("second event = %s, want answered", ev.Type)
	}
	if engine.invites[0].PhoneNumber != "5511999990000" {
		t.Fatalf("dialled %q, want the normalized number", engine.invites[0].PhoneNumber)
	}

	audio := engine.session("engine-call-1")
	audio.frames <- []byte{1, 2}
	select {
	case frame := <-call.AudioStream():
		if len(frame) != 2 {
			t.Fatalf("forwarded frame = %v", frame)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("remote audio never reached the browser leg")
	}
	if err := call.SendAudio([]byte{3, 4}); err != nil {
		t.Fatalf("SendAudio() error = %v", err)
	}
	if audio.writes() != 1 {
		t.Fatalf("browser audio writes = %d, want 1", audio.writes())
	}

	_ = audio.Close()
	if ev := nextEvent(t, call); ev.Type != conversation.CallEventEnded {
		t.Fatalf("event after remote hangup = %s, want ended", ev.Type)
	}
	waitDone(t, call)
}

func TestHangingUpWhileRingingCancelsTheInviteWithoutDialogHangup(t *testing.T) {
	source, engine := newSource(ownedTrunk())
	engine.inviteWait = make(chan struct{})
	call, err := source.Dial(context.Background(), dialInput("100"))
	if err != nil {
		t.Fatal(err)
	}
	nextEvent(t, call)
	if err := call.Hangup(); err != nil {
		t.Fatalf("Hangup() error = %v", err)
	}
	if ev := nextEvent(t, call); ev.Type != conversation.CallEventEnded {
		t.Fatalf("event = %s, want ended", ev.Type)
	}
	waitDone(t, call)
	if engine.hangupCount() != 0 {
		t.Fatal("a ringing call must be cancelled, not hung up on a dialog that never existed")
	}
}

func TestHangingUpAnAnsweredCallEndsTheTrunkLeg(t *testing.T) {
	source, engine := newSource(ownedTrunk())
	call, _ := source.Dial(context.Background(), dialInput("100"))
	nextEvent(t, call)
	nextEvent(t, call)
	if err := call.Hangup(); err != nil {
		t.Fatalf("Hangup() error = %v", err)
	}
	waitDone(t, call)
	if engine.hangupCount() != 1 {
		t.Fatalf("engine hangups = %d, want 1", engine.hangupCount())
	}
	if err := call.SendAudio([]byte{1}); err != nil {
		t.Fatalf("SendAudio() after hangup = %v, want audio silently dropped", err)
	}
}

func TestProviderRejectionsBecomeTheMatchingCallOutcome(t *testing.T) {
	cases := []struct {
		code int
		want conversation.CallEventType
	}{
		{486, conversation.CallEventBusy},
		{600, conversation.CallEventBusy},
		{480, conversation.CallEventNoAnswer},
		{408, conversation.CallEventNoAnswer},
		{603, conversation.CallEventDeclined},
		{404, conversation.CallEventFailed},
		{503, conversation.CallEventFailed},
	}
	for _, tc := range cases {
		source, engine := newSource(ownedTrunk())
		engine.inviteErr = &sip_trunk.CallRejectedError{StatusCode: tc.code, Reason: "Provider"}
		call, err := source.Dial(context.Background(), dialInput("100"))
		if err != nil {
			t.Fatal(err)
		}
		nextEvent(t, call)
		ev := nextEvent(t, call)
		if ev.Type != tc.want || !strings.Contains(ev.Reason, "Provider") {
			t.Errorf("SIP %d became %s (%q), want %s", tc.code, ev.Type, ev.Reason, tc.want)
		}
		waitDone(t, call)
	}
}

func TestAnEngineFailureEndsTheCallAsFailed(t *testing.T) {
	source, engine := newSource(ownedTrunk())
	engine.inviteErr = sip_trunk.ErrTrunkNotRegistered
	call, _ := source.Dial(context.Background(), dialInput("100"))
	nextEvent(t, call)
	if ev := nextEvent(t, call); ev.Type != conversation.CallEventFailed {
		t.Fatalf("event = %s, want failed", ev.Type)
	}
	waitDone(t, call)
}
