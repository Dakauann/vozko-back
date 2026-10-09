package copilotscreen

import (
	"context"
	"testing"
	"time"

	"vozko/domain/copilot"
)

type loopback struct{ handler func([]byte) }

func (l *loopback) Publish(_ string, data []byte) error {
	l.handler(data)
	return nil
}

func (l *loopback) Subscribe(_ context.Context, _ string, handler func([]byte)) { l.handler = handler }

func startedBroker() *Broker {
	b := NewBroker(&loopback{})
	b.Start(context.Background())
	return b
}

func waitBriefly(wait func(context.Context) (copilot.ScreenReply, error)) (copilot.ScreenReply, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	return wait(ctx)
}

func TestAReplyReachesOnlyTheCommandItAnswers(t *testing.T) {
	b := startedBroker()
	mine, cancelMine := b.Expect("th1:cmd-1")
	defer cancelMine()
	theirs, cancelTheirs := b.Expect("th2:cmd-1")
	defer cancelTheirs()

	if err := b.Deliver("th1:cmd-1", copilot.ScreenReply{OK: true, Data: []byte(`1`)}); err != nil {
		t.Fatal(err)
	}
	if reply, err := waitBriefly(mine); err != nil || !reply.OK || string(reply.Data) != "1" {
		t.Fatalf("mine = %+v, %v", reply, err)
	}
	if _, err := waitBriefly(theirs); err == nil {
		t.Fatal("another thread's command must not receive the reply")
	}
}

func TestALateOrRepeatedReplyIsDropped(t *testing.T) {
	b := startedBroker()
	wait, cancel := b.Expect("th1:cmd-1")
	_ = b.Deliver("th1:cmd-1", copilot.ScreenReply{OK: true, Data: []byte(`1`)})
	_ = b.Deliver("th1:cmd-1", copilot.ScreenReply{OK: true, Data: []byte(`2`)})
	if reply, _ := waitBriefly(wait); string(reply.Data) != "1" {
		t.Fatalf("the first reply wins, got %s", reply.Data)
	}
	cancel()
	if err := b.Deliver("th1:cmd-1", copilot.ScreenReply{OK: true}); err != nil {
		t.Fatalf("a reply after the wait ended is ignored, got %v", err)
	}
	if b.waiting() != 0 {
		t.Fatalf("cancel must forget the waiter, %d left", b.waiting())
	}
}

func TestMalformedEnvelopesAreIgnored(t *testing.T) {
	b := startedBroker()
	wait, cancel := b.Expect("th1:cmd-1")
	defer cancel()
	b.deliverLocal([]byte(`not json`))
	b.deliverLocal([]byte(`{"key":"","reply":{"ok":true}}`))
	if _, err := waitBriefly(wait); err == nil {
		t.Fatal("garbage must not wake a waiter")
	}
}

func TestRepliesFromSeveralTabsAreAllHeardInOrder(t *testing.T) {
	b := startedBroker()
	wait, cancel := b.Expect("th1:cmd-1")
	defer cancel()
	_ = b.Deliver("th1:cmd-1", copilot.ScreenReply{Error: &copilot.ScreenError{Code: copilot.ScreenNoEditor}})
	_ = b.Deliver("th1:cmd-1", copilot.ScreenReply{OK: true, Data: []byte(`2`)})
	first, _ := waitBriefly(wait)
	second, err := waitBriefly(wait)
	if !first.Declined() || err != nil || string(second.Data) != "2" {
		t.Fatalf("the decline and the editor's answer must both arrive, got %+v then %+v (%v)", first, second, err)
	}
}
