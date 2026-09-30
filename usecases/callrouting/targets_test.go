package callrouting_usecase

import (
	"context"
	"testing"
	"time"

	"vozko/domain/callrouting"
)

type listedQueues struct {
	queueBook
	list []*callrouting.Queue
}

func (l listedQueues) ListByWorkspace(context.Context, string) ([]*callrouting.Queue, error) {
	return l.list, nil
}

func TestTransferTargetsShowHowBusyEachQueueIs(t *testing.T) {
	ana := &operator{userID: "ana"}
	bia := &operator{userID: "bia", onCall: true}
	caio := &operator{userID: "caio"}
	f := newFixture(ana, bia, caio)
	f.activity.CallEnded("ws1", "caio", time.Now())
	vendas := testQueue("ana", "bia", "caio")
	suporte := testQueue("bia")
	suporte.ID, suporte.Name = "q2", "Suporte"
	suporte.MaxWaitSeconds = 30
	held := newHeldCall("c1")
	go func() { _, _ = f.dispatcher.Enqueue(context.Background(), EnqueueInput{Queue: suporte, Call: held}) }()
	defer held.Hangup()
	waitFor(t, "the caller to wait in Suporte", func() bool { return f.dispatcher.Waiting("q2") == 1 })

	targets, err := NewTransferTargets(listedQueues{list: []*callrouting.Queue{vendas, suporte}}, f.dispatcher).Queues(context.Background(), "ws1")
	if err != nil {
		t.Fatalf("Queues: %v", err)
	}
	want := []QueueTarget{{ID: "q1", Name: "Vendas", Waiting: 0, Ready: 1}, {ID: "q2", Name: "Suporte", Waiting: 1, Ready: 0}}
	if len(targets) != 2 || targets[0] != want[0] || targets[1] != want[1] {
		t.Fatalf("targets = %+v", targets)
	}
}
