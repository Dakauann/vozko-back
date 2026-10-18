package workflow

import (
	"testing"
	"time"
)

type hangingUpCall struct {
	done chan struct{}
}

func newHangingUpCall() hangingUpCall {
	return hangingUpCall{done: make(chan struct{})}
}

func (c hangingUpCall) Play([]byte, bool) (bool, error) { return false, nil }

func (c hangingUpCall) NextKey(time.Duration) (rune, bool, error) { return 0, false, nil }

func (c hangingUpCall) Done() <-chan struct{} { return c.done }

func TestAHangUpEndsTheNodeCallContext(t *testing.T) {
	call := newHangingUpCall()
	ctx, release := (&NodeContext{Runtime: call}).CallContext()
	defer release()

	if ctx.Err() != nil {
		t.Fatal("the context must stay open while the caller is on the line")
	}
	close(call.done)
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("work started during a call must stop when the caller hangs up")
	}
}

func TestOutsideACallTheNodeCallContextLastsUntilReleased(t *testing.T) {
	ctx, release := (&NodeContext{}).CallContext()
	if ctx.Err() != nil {
		t.Fatal("a message run has no caller to hang up")
	}
	release()
	if ctx.Err() == nil {
		t.Fatal("releasing the context must end it")
	}
}

func TestOnlyACallRuntimeRunsInACall(t *testing.T) {
	if InCall(nil) || InCall("not a call") {
		t.Fatal("a run without a call runtime is not in a call")
	}
	if !InCall(newHangingUpCall()) {
		t.Fatal("a call runtime runs in a call")
	}
}

func TestCallEndedReportsTheHangUp(t *testing.T) {
	call := newHangingUpCall()
	if CallEnded(call) {
		t.Fatal("the caller is still on the line")
	}
	close(call.done)
	if !CallEnded(call) {
		t.Fatal("the caller hung up")
	}
	if CallEnded(nil) {
		t.Fatal("a run without a call never reports a hang-up")
	}
}

func TestTheKeyQueueSharesTheCallEnd(t *testing.T) {
	ended := make(chan struct{})
	queue := NewKeyQueue(make(chan rune), ended)
	close(ended)
	select {
	case <-queue.Done():
	default:
		t.Fatal("the key queue must expose the same hang-up signal as the call")
	}
}
