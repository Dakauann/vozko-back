package callrouting_usecase

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"vozko/domain/callrouting"
	"vozko/domain/callsession"
	"vozko/domain/conversation"
)

var errOutOfReach = errors.New("out of reach")

type handoffBook struct {
	mu     sync.Mutex
	denied map[string]bool
	handed []callrouting.Handover
	asked  int
}

func (h *handoffBook) MayHandOver(_ context.Context, handover callrouting.Handover) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.asked++
	if h.denied[handover.ToUserID] {
		return errOutOfReach
	}
	return nil
}

func (h *handoffBook) HandOver(_ context.Context, handover callrouting.Handover) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.handed = append(h.handed, handover)
	return nil
}

func (h *handoffBook) handovers() []callrouting.Handover {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]callrouting.Handover(nil), h.handed...)
}

var customer = conversation.CallContact{BusinessPhoneID: "bp1", ContactNumber: "5584994409684"}

func whatsAppCallOf(call *heldCall) *heldCall {
	call.channel = callsession.OfferChannelWhatsApp
	call.contact = customer
	return call
}

func TestAWarmTransferHandsTheWhatsAppConversationToTheColleague(t *testing.T) {
	f := newTransferFixture(accepts, nil)
	whatsAppCallOf(f.call)
	book := &handoffBook{}
	f.transfers.deps.Dispatcher.SetConversationHandoff(book)

	if _, err := f.transfers.Transfer(context.Background(), TransferInput{
		WorkspaceID: "ws1", UserID: "ana", CallID: "c1",
		Target: callrouting.TransferTarget{Kind: callrouting.TargetMember, UserID: "bia"},
	}); err != nil {
		t.Fatalf("Transfer: %v", err)
	}
	waitFor(t, "the conversation to follow the call", func() bool { return len(book.handovers()) == 1 })
	want := callrouting.Handover{WorkspaceID: "ws1", Contact: customer, FromUserID: "ana", ToUserID: "bia"}
	if got := book.handovers()[0]; got != want {
		t.Fatalf("handover = %+v, want %+v", got, want)
	}
}

func TestAWhatsAppCallCannotGoToSomeoneTheConversationCannotFollow(t *testing.T) {
	f := newTransferFixture(accepts, nil)
	whatsAppCallOf(f.call)
	f.transfers.deps.Dispatcher.SetConversationHandoff(&handoffBook{denied: map[string]bool{"bia": true}})

	_, err := f.transfers.Transfer(context.Background(), TransferInput{
		WorkspaceID: "ws1", UserID: "ana", CallID: "c1",
		Target: callrouting.TransferTarget{Kind: callrouting.TargetMember, UserID: "bia"},
	})
	if !errors.Is(err, callrouting.ErrConversationOutOfReach) {
		t.Fatalf("err = %v", err)
	}
	if len(f.bia.receivedOffers()) != 0 || f.call.held != 0 {
		t.Fatal("a refused transfer still moved the caller")
	}
}

func TestWhatsAppTransfersAreRefusedWhenConversationsCannotFollow(t *testing.T) {
	f := newTransferFixture(accepts, nil)
	whatsAppCallOf(f.call)

	_, err := f.transfers.Transfer(context.Background(), TransferInput{
		WorkspaceID: "ws1", UserID: "ana", CallID: "c1",
		Target: callrouting.TransferTarget{Kind: callrouting.TargetMember, UserID: "bia"},
	})
	if !errors.Is(err, callrouting.ErrConversationOutOfReach) {
		t.Fatalf("without a handoff err = %v", err)
	}
}

func TestAQueueOnlyRingsMembersTheWhatsAppConversationCanFollow(t *testing.T) {
	bia := &operator{userID: "bia"}
	caio := &operator{userID: "caio"}
	f := newFixture(bia, caio)
	f.activity.CallEnded("ws1", "caio", time.Now().Add(-time.Minute))
	f.activity.CallEnded("ws1", "bia", time.Now().Add(-time.Hour))
	book := &handoffBook{denied: map[string]bool{"bia": true}}
	f.dispatcher.SetConversationHandoff(book)
	call := whatsAppCallOf(newHeldCall("wa-in-1"))

	result, _ := f.dispatcher.Enqueue(context.Background(), EnqueueInput{Queue: testQueue("bia", "caio"), Call: call, FromUserID: "ana"})
	if result.AgentUserID != "caio" || len(bia.receivedOffers()) != 0 {
		t.Fatalf("result = %+v, bia offers = %d", result, len(bia.receivedOffers()))
	}
	want := callrouting.Handover{WorkspaceID: "ws1", Contact: customer, FromUserID: "ana", ToUserID: "caio"}
	if got := book.handovers(); len(got) != 1 || got[0] != want {
		t.Fatalf("handovers = %+v", got)
	}
}

func TestSIPCallsNeverTouchConversations(t *testing.T) {
	f := newTransferFixture(accepts, nil)
	book := &handoffBook{denied: map[string]bool{"bia": true}}
	f.transfers.deps.Dispatcher.SetConversationHandoff(book)

	if _, err := f.transfers.Transfer(context.Background(), TransferInput{
		WorkspaceID: "ws1", UserID: "ana", CallID: "c1",
		Target: callrouting.TransferTarget{Kind: callrouting.TargetMember, UserID: "bia"},
	}); err != nil {
		t.Fatalf("Transfer: %v", err)
	}
	waitFor(t, "bia to take the SIP call", ownedBy(f.call, "bia"))
	if book.asked != 0 || len(book.handovers()) != 0 {
		t.Fatal("a SIP call consulted the conversation handoff")
	}
}

func TestACallReturningToItsOperatorKeepsTheConversationWhereItIs(t *testing.T) {
	f := newTransferFixture(declines, nil)
	whatsAppCallOf(f.call)
	book := &handoffBook{}
	f.transfers.deps.Dispatcher.SetConversationHandoff(book)

	_, _ = f.transfers.Transfer(context.Background(), TransferInput{
		WorkspaceID: "ws1", UserID: "ana", CallID: "c1",
		Target: callrouting.TransferTarget{Kind: callrouting.TargetMember, UserID: "bia"},
	})
	waitFor(t, "the caller to come back to ana", ownedBy(f.call, "ana"))
	if len(book.handovers()) != 0 {
		t.Fatalf("handovers = %+v", book.handovers())
	}
}
