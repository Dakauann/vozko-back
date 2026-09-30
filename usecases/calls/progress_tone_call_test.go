package calls_usecase

import (
	"bytes"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/voip"
)

var quickTones = ProgressTonePlan{
	Ringback:   voip.BrazilRingback,
	Busy:       voip.BrazilBusy,
	Congestion: voip.BrazilCongestion,
	Closing:    200 * time.Millisecond,
}

func ringbackFrame() []byte { return voip.NewToneSource(voip.BrazilRingback).Next() }

func receiveAudio(t *testing.T, call *ProgressToneCall, within time.Duration) []byte {
	t.Helper()
	select {
	case frame := <-call.AudioStream():
		return frame
	case <-time.After(within):
		return nil
	}
}

func receiveEvent(t *testing.T, call *ProgressToneCall) conversation.CallEvent {
	t.Helper()
	select {
	case event := <-call.Events():
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("no call event arrived")
		return conversation.CallEvent{}
	}
}

func drainAudio(call *ProgressToneCall) int {
	frames := 0
	for {
		select {
		case <-call.AudioStream():
			frames++
		case <-time.After(60 * time.Millisecond):
			return frames
		}
	}
}

func TestNothingPlaysBeforeTheFarEndIsAlerting(t *testing.T) {
	inner := newFakeCRMCall("c1")
	call := NewProgressToneCall(inner, quickTones)
	inner.events <- conversation.CallEvent{Type: conversation.CallEventRinging}

	if got := receiveEvent(t, call); got.Type != conversation.CallEventRinging {
		t.Fatalf("event = %+v, want ringing forwarded", got)
	}
	if frame := receiveAudio(t, call, 100*time.Millisecond); frame != nil {
		t.Fatal("ringback played before the far end was alerting")
	}
}

func TestRingbackPlaysWhileTheFarEndIsAlertingAndStopsOnAnswer(t *testing.T) {
	inner := newFakeCRMCall("c1")
	call := NewProgressToneCall(inner, quickTones)
	inner.events <- conversation.CallEvent{Type: conversation.CallEventAlerting}

	frame := receiveAudio(t, call, time.Second)
	if frame == nil || !bytes.Equal(frame, ringbackFrame()) {
		t.Fatal("the operator did not hear the standard ringback")
	}
	inner.events <- conversation.CallEvent{Type: conversation.CallEventAnswered}
	if got := receiveEvent(t, call); got.Type != conversation.CallEventAnswered {
		t.Fatalf("event = %+v, want answered; alerting must stay internal", got)
	}
	drainAudio(call)
	if frame := receiveAudio(t, call, 100*time.Millisecond); frame != nil {
		t.Fatal("ringback kept playing after the answer")
	}
}

func TestTheCarriersOwnEarlyMediaReplacesRingback(t *testing.T) {
	inner := newFakeCRMCall("c1")
	call := NewProgressToneCall(inner, quickTones)
	inner.events <- conversation.CallEvent{Type: conversation.CallEventAlerting}
	receiveAudio(t, call, time.Second)

	announcement := bytes.Repeat([]byte{7}, voip.PCMFrameBytes)
	inner.audioIn <- announcement
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if frame := receiveAudio(t, call, 100*time.Millisecond); bytes.Equal(frame, announcement) {
			break
		}
	}
	drainAudio(call)
	if frame := receiveAudio(t, call, 100*time.Millisecond); frame != nil {
		t.Fatal("ringback talked over the carrier's announcement")
	}
}

func TestEarlyMediaBeforeAlertingNeverStartsRingback(t *testing.T) {
	inner := newFakeCRMCall("c1")
	call := NewProgressToneCall(inner, quickTones)
	announcement := bytes.Repeat([]byte{7}, voip.PCMFrameBytes)
	inner.audioIn <- announcement
	if frame := receiveAudio(t, call, time.Second); !bytes.Equal(frame, announcement) {
		t.Fatal("early media was not passed through")
	}
	inner.events <- conversation.CallEvent{Type: conversation.CallEventAlerting}
	if frame := receiveAudio(t, call, 100*time.Millisecond); frame != nil {
		t.Fatal("ringback started although the carrier already sends audio")
	}
}

func TestABusyCallPlaysTheBusyToneBeforeEnding(t *testing.T) {
	inner := newFakeCRMCall("c1")
	call := NewProgressToneCall(inner, quickTones)
	inner.events <- conversation.CallEvent{Type: conversation.CallEventBusy, Reason: "486 Busy Here"}

	busy := voip.NewToneSource(voip.BrazilBusy).Next()
	if frame := receiveAudio(t, call, time.Second); !bytes.Equal(frame, busy) {
		t.Fatal("the busy tone did not play")
	}
	if got := receiveEvent(t, call); got.Type != conversation.CallEventBusy || got.Reason != "486 Busy Here" {
		t.Fatalf("event = %+v, want the busy outcome after the tone", got)
	}
	<-call.Done()
}

func TestADeclinedCallSoundsBusyAndAFailedOneSoundsCongested(t *testing.T) {
	cases := map[conversation.CallEventType]voip.Tone{
		conversation.CallEventDeclined: voip.BrazilBusy,
		conversation.CallEventFailed:   voip.BrazilCongestion,
	}
	for outcome, tone := range cases {
		inner := newFakeCRMCall("c1")
		call := NewProgressToneCall(inner, quickTones)
		inner.events <- conversation.CallEvent{Type: outcome}
		if frame := receiveAudio(t, call, time.Second); !bytes.Equal(frame, voip.NewToneSource(tone).Next()) {
			t.Fatalf("%s did not play its tone", outcome)
		}
		receiveEvent(t, call)
	}
}

func TestCallsThatEndNormallyEndInSilence(t *testing.T) {
	for _, outcome := range []conversation.CallEventType{conversation.CallEventEnded, conversation.CallEventNoAnswer} {
		inner := newFakeCRMCall("c1")
		call := NewProgressToneCall(inner, quickTones)
		inner.events <- conversation.CallEvent{Type: outcome}
		if got := receiveEvent(t, call); got.Type != outcome {
			t.Fatalf("event = %+v", got)
		}
		if frame := receiveAudio(t, call, 50*time.Millisecond); frame != nil {
			t.Fatalf("%s played a tone", outcome)
		}
	}
}

func TestHangingUpCutsTheClosingToneShort(t *testing.T) {
	inner := newFakeCRMCall("c1")
	slow := quickTones
	slow.Closing = 10 * time.Second
	call := NewProgressToneCall(inner, slow)
	inner.events <- conversation.CallEvent{Type: conversation.CallEventBusy}
	receiveAudio(t, call, time.Second)

	if err := call.Hangup(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-call.Done():
	case <-time.After(time.Second):
		t.Fatal("the busy tone kept the call open after hanging up")
	}
	inner.mu.Lock()
	defer inner.mu.Unlock()
	if inner.hangups != 1 {
		t.Fatalf("inner hangups = %d, want 1", inner.hangups)
	}
}

func TestTheWrapperKeepsTheCallsIdentityAndVoice(t *testing.T) {
	inner := newFakeCRMCall("c1")
	call := NewProgressToneCall(inner, quickTones)
	if call.ID() != "c1" {
		t.Fatalf("ID = %q", call.ID())
	}
	if err := call.SendAudio([]byte{1, 2}); err != nil {
		t.Fatal(err)
	}
	inner.mu.Lock()
	sent := len(inner.sent)
	inner.mu.Unlock()
	if sent != 1 {
		t.Fatal("the operator's voice did not reach the call")
	}
	close(inner.events)
	<-call.Done()
}

func TestTheOutcomeStillArrivesAfterTheOperatorHangsUp(t *testing.T) {
	for range 50 {
		inner := newFakeCRMCall("c1")
		call := NewProgressToneCall(inner, quickTones)
		_ = call.Hangup()
		inner.events <- conversation.CallEvent{Type: conversation.CallEventEnded, Reason: "cancelled"}
		if got := receiveEvent(t, call); got.Type != conversation.CallEventEnded {
			t.Fatalf("event = %+v, want the ended outcome", got)
		}
		<-call.Done()
	}
}

func TestACallThatOnlySignalsDoneStillEnds(t *testing.T) {
	inner := newFakeCRMCall("c1")
	call := NewProgressToneCall(inner, quickTones)
	inner.events <- conversation.CallEvent{Type: conversation.CallEventAlerting}
	receiveAudio(t, call, time.Second)
	close(inner.done)
	select {
	case <-call.Done():
	case <-time.After(time.Second):
		t.Fatal("the wrapper outlived a call that ended through Done")
	}
}

func TestAnOutcomeWaitingBehindDoneKeepsItsTone(t *testing.T) {
	inner := newFakeCRMCall("c1")
	inner.events <- conversation.CallEvent{Type: conversation.CallEventBusy}
	close(inner.done)
	call := NewProgressToneCall(inner, quickTones)
	for event := range call.Events() {
		if event.Type == conversation.CallEventBusy {
			return
		}
	}
	t.Fatal("the busy outcome was lost")
}

func TestSilentEarlyMediaNeitherStopsNorDoublesTheRingback(t *testing.T) {
	inner := newFakeCRMCall("c1")
	call := NewProgressToneCall(inner, quickTones)
	inner.events <- conversation.CallEvent{Type: conversation.CallEventAlerting}
	receiveAudio(t, call, time.Second)

	silence := make([]byte, voip.PCMFrameBytes)
	for range 10 {
		inner.audioIn <- silence
	}
	started := time.Now()
	frames := 0
	for time.Since(started) < 400*time.Millisecond {
		frame := receiveAudio(t, call, 100*time.Millisecond)
		if frame == nil {
			t.Fatal("silent early media stopped the ringback")
		}
		if bytes.Equal(frame, silence) {
			t.Fatal("silent early media was interleaved with the ringback")
		}
		frames++
	}
	if frames > 25 {
		t.Fatalf("%d frames in 400ms: the operator would hear audio at the wrong speed", frames)
	}
}
