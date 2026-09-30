package callrouting_usecase

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"vozko/domain/callrouting"
	"vozko/domain/callsession"
	callsession_usecase "vozko/usecases/callsession"
)

type callBook map[string]*heldCall

func (b callBook) Find(workspaceID, callID string) (callrouting.RoutedCall, bool) {
	call, ok := b[callID]
	if !ok || workspaceID != "ws1" {
		return nil, false
	}
	return call, true
}

type queueBook map[string]*callrouting.Queue

func (q queueBook) Create(context.Context, *callrouting.Queue) error { return nil }
func (q queueBook) Update(context.Context, *callrouting.Queue) error { return nil }
func (q queueBook) Delete(context.Context, string, string) error     { return nil }
func (q queueBook) ListByWorkspace(context.Context, string) ([]*callrouting.Queue, error) {
	return nil, nil
}
func (q queueBook) FindInWorkspace(_ context.Context, workspaceID, id string) (*callrouting.Queue, error) {
	queue, ok := q[id]
	if !ok || queue.WorkspaceID != workspaceID {
		return nil, callrouting.ErrQueueNotFound
	}
	return queue, nil
}

type transferBook struct {
	mu       sync.Mutex
	records  []callrouting.TransferRecord
	outcomes map[string]callrouting.TransferOutcome
	answered map[string]string
}

func newTransferBook() *transferBook {
	return &transferBook{outcomes: map[string]callrouting.TransferOutcome{}, answered: map[string]string{}}
}

func (b *transferBook) Record(_ context.Context, record callrouting.TransferRecord) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.records = append(b.records, record)
	return nil
}

func (b *transferBook) Finish(_ context.Context, _ string, id string, outcome callrouting.TransferOutcome, answeredBy string, _ time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.outcomes[id] = outcome
	b.answered[id] = answeredBy
	return nil
}

func (b *transferBook) outcome(id string) callrouting.TransferOutcome {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.outcomes[id]
}

type names map[string]string

func (n names) ResolveUsernames(ids []string) map[string]string { return n }

type transferFixture struct {
	transfers *TransferCall
	call      *heldCall
	log       *transferBook
	ana, bia  *operator
}

func newTransferFixture(biaBehaviour operatorBehaviour, queues queueBook) transferFixture {
	ana := &operator{userID: "ana", onCall: true}
	bia := &operator{userID: "bia", behaviour: biaBehaviour}
	dani := &operator{userID: "dani"}
	f := newFixtureWith(answerPermission{"dani": false}, ana, bia, dani)
	call := newHeldCall("c1")
	call.owner = ana
	book := newTransferBook()
	transfers := NewTransferCall(TransferDeps{
		Calls:      callBook{"c1": call},
		Queues:     queues,
		Dispatcher: f.dispatcher,
		Ringer:     callsession_usecase.NewInboundRinger(f.broker),
		Music:      music{},
		Log:        book,
		Names:      names{"ana": "Ana Souza"},
	})
	transfers.ringTimeout = 300 * time.Millisecond
	return transferFixture{transfers: transfers, call: call, log: book, ana: ana, bia: bia}
}

func waitFor(t *testing.T, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func ownedBy(call *heldCall, userID string) func() bool {
	return func() bool {
		owner, ok := call.OwnerSession()
		return ok && owner.UserID() == userID
	}
}

func TestAWarmTransferHandsTheCallerToTheColleagueWithTheNotes(t *testing.T) {
	f := newTransferFixture(accepts, nil)

	id, err := f.transfers.Transfer(context.Background(), TransferInput{
		WorkspaceID: "ws1", UserID: "ana", CallID: "c1",
		Target: callrouting.TransferTarget{Kind: callrouting.TargetMember, UserID: "bia"},
		Notes:  "cliente quer cancelar o plano",
	})
	if err != nil {
		t.Fatalf("Transfer: %v", err)
	}
	waitFor(t, "bia to own the call", ownedBy(f.call, "bia"))
	waitFor(t, "the transfer to be recorded as connected", func() bool { return f.log.outcome(id) == callrouting.OutcomeConnected })

	offer := f.bia.receivedOffers()[0]
	if offer.Transfer == nil || offer.Transfer.FromName != "Ana Souza" || offer.Transfer.Notes != "cliente quer cancelar o plano" {
		t.Fatalf("offer transfer context = %+v", offer.Transfer)
	}
	if got := f.ana.transferStatuses(); !slices.Equal(got, []string{callsession.TransferStatusRinging, callsession.TransferStatusConnected}) {
		t.Fatalf("ana saw %v", got)
	}
	if f.ana.isOnCall() {
		t.Fatal("ana still holds a call she handed over")
	}
}

func TestAColleagueWhoDeclinesSendsTheCallerBack(t *testing.T) {
	f := newTransferFixture(declines, nil)
	id, _ := f.transfers.Transfer(context.Background(), TransferInput{
		WorkspaceID: "ws1", UserID: "ana", CallID: "c1",
		Target: callrouting.TransferTarget{Kind: callrouting.TargetMember, UserID: "bia"},
	})
	waitFor(t, "the caller to come back to ana", ownedBy(f.call, "ana"))
	waitFor(t, "the transfer to be recorded as returned", func() bool { return f.log.outcome(id) == callrouting.OutcomeReturned })
	if got := f.ana.transferStatuses(); got[len(got)-1] != callsession.TransferStatusReturned {
		t.Fatalf("ana saw %v", got)
	}
}

func TestCancellingATransferBringsTheCallerBack(t *testing.T) {
	f := newTransferFixture(ignores, nil)
	f.transfers.ringTimeout = 5 * time.Second
	id, _ := f.transfers.Transfer(context.Background(), TransferInput{
		WorkspaceID: "ws1", UserID: "ana", CallID: "c1",
		Target: callrouting.TransferTarget{Kind: callrouting.TargetMember, UserID: "bia"},
	})
	waitFor(t, "bia to be rung", func() bool { return len(f.bia.receivedOffers()) == 1 })

	if err := f.transfers.Cancel("ws1", "bia", "c1"); !errors.Is(err, callrouting.ErrNoTransferToCancel) {
		t.Fatalf("someone else cancelled ana's transfer: %v", err)
	}
	if err := f.transfers.Cancel("ws1", "ana", "c1"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	waitFor(t, "the caller to come back to ana", ownedBy(f.call, "ana"))
	waitFor(t, "the transfer to be recorded as cancelled", func() bool { return f.log.outcome(id) == callrouting.OutcomeCancelled })
}

func TestATransferToAQueueConnectsAQueueMember(t *testing.T) {
	queue := testQueue("bia")
	f := newTransferFixture(accepts, queueBook{"q1": queue})
	id, err := f.transfers.Transfer(context.Background(), TransferInput{
		WorkspaceID: "ws1", UserID: "ana", CallID: "c1",
		Target: callrouting.TransferTarget{Kind: callrouting.TargetQueue, QueueID: "q1"},
		Notes:  "segunda via",
	})
	if err != nil {
		t.Fatalf("Transfer: %v", err)
	}
	waitFor(t, "bia to own the call", ownedBy(f.call, "bia"))
	waitFor(t, "the transfer to be recorded as connected", func() bool { return f.log.outcome(id) == callrouting.OutcomeConnected })
	if got := f.ana.transferStatuses(); got[0] != callsession.TransferStatusQueued {
		t.Fatalf("ana saw %v", got)
	}
}

func TestTransfersAreRefusedWhenTheyCannotBeSafe(t *testing.T) {
	long := strings.Repeat("a", callrouting.MaxTransferNotes+1)
	cases := []struct {
		name  string
		input TransferInput
		want  error
	}{
		{"someone else's call", TransferInput{WorkspaceID: "ws1", UserID: "bia", CallID: "c1", Target: callrouting.TransferTarget{Kind: callrouting.TargetMember, UserID: "ana"}}, callrouting.ErrNotCallOwner},
		{"unknown call", TransferInput{WorkspaceID: "ws1", UserID: "ana", CallID: "nope", Target: callrouting.TransferTarget{Kind: callrouting.TargetMember, UserID: "bia"}}, callrouting.ErrCallNotFound},
		{"another workspace", TransferInput{WorkspaceID: "ws2", UserID: "ana", CallID: "c1", Target: callrouting.TransferTarget{Kind: callrouting.TargetMember, UserID: "bia"}}, callrouting.ErrCallNotFound},
		{"to yourself", TransferInput{WorkspaceID: "ws1", UserID: "ana", CallID: "c1", Target: callrouting.TransferTarget{Kind: callrouting.TargetMember, UserID: "ana"}}, callrouting.ErrTransferToSelf},
		{"colleague offline", TransferInput{WorkspaceID: "ws1", UserID: "ana", CallID: "c1", Target: callrouting.TransferTarget{Kind: callrouting.TargetMember, UserID: "caio"}}, callrouting.ErrTargetUnavailable},
		{"unknown queue", TransferInput{WorkspaceID: "ws1", UserID: "ana", CallID: "c1", Target: callrouting.TransferTarget{Kind: callrouting.TargetQueue, QueueID: "nope"}}, callrouting.ErrQueueNotFound},
		{"notes too long", TransferInput{WorkspaceID: "ws1", UserID: "ana", CallID: "c1", Target: callrouting.TransferTarget{Kind: callrouting.TargetMember, UserID: "bia"}, Notes: long}, callrouting.ErrTransferNotesTooLong},
		{"no target", TransferInput{WorkspaceID: "ws1", UserID: "ana", CallID: "c1"}, callrouting.ErrInvalidTransferTarget},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newTransferFixture(accepts, queueBook{})
			if _, err := f.transfers.Transfer(context.Background(), tc.input); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if owner, _ := f.call.OwnerSession(); owner.UserID() != "ana" {
				t.Fatal("a refused transfer still moved the caller")
			}
		})
	}
}

func TestACallCanOnlyHaveOneTransferAtATime(t *testing.T) {
	f := newTransferFixture(ignores, nil)
	f.transfers.ringTimeout = 5 * time.Second
	input := TransferInput{WorkspaceID: "ws1", UserID: "ana", CallID: "c1", Target: callrouting.TransferTarget{Kind: callrouting.TargetMember, UserID: "bia"}}
	if _, err := f.transfers.Transfer(context.Background(), input); err != nil {
		t.Fatalf("first: %v", err)
	}
	f.call.mu.Lock()
	f.call.owner = f.ana
	f.call.mu.Unlock()
	if _, err := f.transfers.Transfer(context.Background(), input); !errors.Is(err, callrouting.ErrTransferInProgress) {
		t.Fatalf("second err = %v", err)
	}
	_ = f.transfers.Cancel("ws1", "ana", "c1")
}
