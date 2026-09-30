package callrouting_usecase

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"vozko/domain/callrouting"
	"vozko/domain/callsession"
)

func reconnectFixture(anaBehaviour operatorBehaviour, grace time.Duration) (transferFixture, *heldCall) {
	f := newTransferFixture(accepts, nil)
	f.ana.behaviour = anaBehaviour
	f.ana.mu.Lock()
	f.ana.onCall = false
	f.ana.mu.Unlock()
	f.transfers.reconnectGrace = grace
	call := newHeldCall("c-orphan")
	return f, call
}

func goroutinesIn(function string) int {
	buf := make([]byte, 1<<20)
	return strings.Count(string(buf[:runtime.Stack(buf, true)]), function)
}

func nothingLeftWaiting(t *testing.T) {
	t.Helper()
	waitFor(t, "every reconnect wait to finish", func() bool { return goroutinesIn("(*TransferCall).awaitOwner") == 0 })
}

func isDone(call *heldCall) bool {
	select {
	case <-call.Done():
		return true
	default:
		return false
	}
}

func TestAnOperatorWhoComesBackResumesTheirCall(t *testing.T) {
	defer nothingLeftWaiting(t)
	f, call := reconnectFixture(accepts, 3*time.Second)
	if err := f.transfers.HoldForOwner(context.Background(), call, "ana"); err != nil {
		t.Fatalf("HoldForOwner: %v", err)
	}
	waitFor(t, "ana to get the call back", ownedBy(call, "ana"))
	offers := f.ana.receivedOffers()
	if len(offers) != 1 || !offers[0].Resume || offers[0].Channel != callsession.OfferChannelSIP || offers[0].Transfer != nil {
		t.Fatalf("offers = %+v", offers)
	}
	if call.held != 1 || isDone(call) {
		t.Fatal("the caller should hold and then stay on the line")
	}
}

func TestACallerIsNotKeptWaitingForAnOperatorWhoNeverReturns(t *testing.T) {
	defer nothingLeftWaiting(t)
	f, call := reconnectFixture(accepts, 150*time.Millisecond)
	f.ana.mu.Lock()
	f.ana.onCall = true
	f.ana.mu.Unlock()
	_ = f.transfers.HoldForOwner(context.Background(), call, "ana")
	waitFor(t, "the call to be hung up after the grace", func() bool { return isDone(call) })
	if len(f.ana.receivedOffers()) != 0 {
		t.Fatal("a busy operator was offered the call")
	}
}

func TestAnOperatorWhoDeclinesTheResumeEndsTheCall(t *testing.T) {
	defer nothingLeftWaiting(t)
	f, call := reconnectFixture(declines, 3*time.Second)
	_ = f.transfers.HoldForOwner(context.Background(), call, "ana")
	waitFor(t, "the call to be hung up", func() bool { return isDone(call) })
	if owner, owned := call.OwnerSession(); owned {
		t.Fatalf("the call went to %s", owner.UserID())
	}
}

func TestOnlyTheSameOperatorIsOfferedTheCallBack(t *testing.T) {
	defer nothingLeftWaiting(t)
	f, call := reconnectFixture(accepts, 200*time.Millisecond)
	f.ana.mu.Lock()
	f.ana.onCall = true
	f.ana.mu.Unlock()
	_ = f.transfers.HoldForOwner(context.Background(), call, "ana")
	waitFor(t, "the call to be hung up", func() bool { return isDone(call) })
	if len(f.bia.receivedOffers()) != 0 {
		t.Fatal("a colleague was offered someone else's call")
	}
}

func TestACallerWhoHangsUpDuringTheGraceIsLetGo(t *testing.T) {
	defer nothingLeftWaiting(t)
	f, call := reconnectFixture(ignores, 3*time.Second)
	f.ana.mu.Lock()
	f.ana.onCall = true
	f.ana.mu.Unlock()
	_ = f.transfers.HoldForOwner(context.Background(), call, "ana")
	_ = call.Hangup()
	time.Sleep(100 * time.Millisecond)
	if len(f.ana.receivedOffers()) != 0 {
		t.Fatal("an operator was offered a call nobody is on")
	}
}

func TestAWarmTransferComesBackToTheOperatorsNewSession(t *testing.T) {
	defer nothingLeftWaiting(t)
	f := newTransferFixture(declines, nil)
	f.ana.mu.Lock()
	f.ana.closed = true
	f.ana.mu.Unlock()
	anaAgain := &operator{userID: "ana", id: "session-ana-2", broker: f.ana.broker}
	f.transfers.deps.Dispatcher.deps.Sessions = presence{operators: []*operator{anaAgain, f.bia}}

	id, err := f.transfers.Transfer(context.Background(), TransferInput{
		WorkspaceID: "ws1", UserID: "ana", CallID: "c1",
		Target: callrouting.TransferTarget{Kind: callrouting.TargetMember, UserID: "bia"},
	})
	if err != nil {
		t.Fatalf("Transfer: %v", err)
	}
	waitFor(t, "the caller to reach ana's new session", func() bool {
		owner, ok := f.call.OwnerSession()
		return ok && owner == anaAgain
	})
	if offers := anaAgain.receivedOffers(); len(offers) != 1 || !offers[0].Resume {
		t.Fatalf("ana's new session got %+v", offers)
	}
	waitFor(t, "the transfer to be logged as returned", func() bool { return f.log.outcome(id) == callrouting.OutcomeReturned })
}

func TestWhatsAppCallsOnlyGoToPeopleWhoMayTakeThem(t *testing.T) {
	f := newTransferFixture(accepts, queueBook{"q1": testQueue("bia", "caio")})
	f.call.channel = callsession.OfferChannelWhatsApp
	perms := f.transfers.deps.Dispatcher.deps.Permission.(answerPermission)
	perms["bia|whatsapp"] = false

	_, err := f.transfers.Transfer(context.Background(), TransferInput{
		WorkspaceID: "ws1", UserID: "ana", CallID: "c1",
		Target: callrouting.TransferTarget{Kind: callrouting.TargetMember, UserID: "bia"},
	})
	if !errors.Is(err, callrouting.ErrTargetUnavailable) {
		t.Fatalf("err = %v, want target unavailable", err)
	}

	perms["ana|whatsapp"] = false
	_, err = f.transfers.Transfer(context.Background(), TransferInput{
		WorkspaceID: "ws1", UserID: "ana", CallID: "c1",
		Target: callrouting.TransferTarget{Kind: callrouting.TargetQueue, QueueID: "q1"},
	})
	if !errors.Is(err, callrouting.ErrTransferNotAllowed) {
		t.Fatalf("err = %v, want not allowed", err)
	}
}

func TestAWhatsAppCallInAQueueSkipsMembersWhoMayNotTakeIt(t *testing.T) {
	bia := &operator{userID: "bia"}
	caio := &operator{userID: "caio"}
	f := newFixtureWith(answerPermission{"bia|whatsapp": false}, bia, caio)
	f.activity.CallEnded("ws1", "caio", time.Now().Add(-time.Hour))
	f.dispatcher.SetConversationHandoff(&handoffBook{})
	call := whatsAppCallOf(newHeldCall("wa-in-1"))
	queue := testQueue("bia", "caio")
	queue.WrapUpSeconds = 0

	result, _ := f.dispatcher.Enqueue(context.Background(), EnqueueInput{Queue: queue, Call: call, FromUserID: "ana"})
	if result.AgentUserID != "caio" || len(bia.receivedOffers()) != 0 {
		t.Fatalf("result = %+v, bia offers = %d", result, len(bia.receivedOffers()))
	}
	if offer := caio.receivedOffers()[0]; offer.Channel != callsession.OfferChannelWhatsApp {
		t.Fatalf("caio's offer channel = %q", offer.Channel)
	}
}

func TestOnlyPeopleAllowedToTransferCanHandACallOver(t *testing.T) {
	f := newTransferFixture(accepts, nil)
	f.transfers.deps.Permission.(answerPermission)["ana|transfer"] = false
	_, err := f.transfers.Transfer(context.Background(), TransferInput{
		WorkspaceID: "ws1", UserID: "ana", CallID: "c1",
		Target: callrouting.TransferTarget{Kind: callrouting.TargetMember, UserID: "bia"},
	})
	if !errors.Is(err, callrouting.ErrTransferNotAllowed) {
		t.Fatalf("err = %v", err)
	}
	f.transfers.deps.Permission = nil
	if _, err := f.transfers.Transfer(context.Background(), TransferInput{
		WorkspaceID: "ws1", UserID: "ana", CallID: "c1",
		Target: callrouting.TransferTarget{Kind: callrouting.TargetMember, UserID: "bia"},
	}); !errors.Is(err, callrouting.ErrTransferNotAllowed) {
		t.Fatalf("without a permission check err = %v", err)
	}
	if len(f.bia.receivedOffers()) != 0 {
		t.Fatal("a refused transfer still rang the colleague")
	}
}
