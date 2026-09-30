package callrouting_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/callrouting"
)

func newEntryFixture(biaBehaviour operatorBehaviour, queue *callrouting.Queue) (transferFixture, *heldCall) {
	f := newTransferFixture(biaBehaviour, queueBook{queue.ID: queue})
	parked := newHeldCall("flow-call")
	return f, parked
}

func TestAWorkflowCallerIsConnectedToAQueueMember(t *testing.T) {
	f, parked := newEntryFixture(accepts, testQueue("bia"))

	outcome, err := f.transfers.EnterQueue(context.Background(), callrouting.QueueEntry{
		WorkspaceID: "ws1", QueueID: "q1", Notes: "Escolheu suporte", From: "URA principal", Call: parked,
	})
	if err != nil || outcome != callrouting.OutcomeConnected {
		t.Fatalf("outcome = %s, %v", outcome, err)
	}
	if owner, ok := parked.OwnerSession(); !ok || owner.UserID() != "bia" {
		t.Fatal("bia does not own the caller")
	}
	offer := f.bia.receivedOffers()[0]
	if offer.Transfer == nil || offer.Transfer.FromName != "URA principal" || offer.Transfer.Notes != "Escolheu suporte" || offer.Transfer.QueueName != "Vendas" {
		t.Fatalf("offer transfer context = %+v", offer.Transfer)
	}
	if len(f.log.records) != 1 || f.log.outcome(f.log.records[0].ID) != callrouting.OutcomeConnected {
		t.Fatalf("log = %+v", f.log.records)
	}
}

func TestAWorkflowCallerNobodyAnswersGetsTheLineBack(t *testing.T) {
	f, parked := newEntryFixture(ignores, testQueue("bia"))

	outcome, err := f.transfers.EnterQueue(context.Background(), callrouting.QueueEntry{WorkspaceID: "ws1", QueueID: "q1", Call: parked})
	if err != nil || outcome != callrouting.OutcomeTimedOut {
		t.Fatalf("outcome = %s, %v", outcome, err)
	}
	if parked.unheld == 0 {
		t.Fatal("hold music kept playing over the rest of the flow")
	}
	select {
	case <-parked.Done():
		t.Fatal("the caller was hung up instead of going back to the flow")
	default:
	}
	if f.log.outcome(f.log.records[0].ID) != callrouting.OutcomeTimedOut {
		t.Fatal("the timeout was not logged")
	}
}

func TestAWorkflowCannotQueueWhatIsNotItsOwn(t *testing.T) {
	f, parked := newEntryFixture(accepts, testQueue("bia"))
	cases := map[string]struct {
		entry callrouting.QueueEntry
		want  error
	}{
		"unknown queue":     {callrouting.QueueEntry{WorkspaceID: "ws1", QueueID: "nope", Call: parked}, callrouting.ErrQueueNotFound},
		"another workspace": {callrouting.QueueEntry{WorkspaceID: "ws2", QueueID: "q1", Call: parked}, callrouting.ErrCallNotFound},
		"no call":           {callrouting.QueueEntry{WorkspaceID: "ws1", QueueID: "q1"}, callrouting.ErrCallNotFound},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := f.transfers.EnterQueue(context.Background(), tc.entry); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
	if len(f.log.records) != 0 || parked.held != 0 {
		t.Fatal("a refused entry still touched the call")
	}
}
