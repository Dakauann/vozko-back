package callsession_usecase

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"vozko/domain/calls/cdr"
	"vozko/domain/callsession"
	"vozko/domain/conversation"
)

type startedCalls struct {
	mu     sync.Mutex
	inputs []cdr.StartCallInput
	err    error
}

func (s *startedCalls) Execute(input cdr.StartCallInput) (*cdr.Call, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inputs = append(s.inputs, input)
	if s.err != nil {
		return nil, s.err
	}
	return &cdr.Call{ID: "record-1", CallID: input.CallID}, nil
}

type stampedItems struct {
	mu       sync.Mutex
	stamped  []string
	attempts int
	failures int
	refusal  error
}

func (s *stampedItems) CheckItemDial(context.Context, callsession.CallListItemDial) error {
	return nil
}

func (s *stampedItems) StampLastCall(_ context.Context, stamp callsession.CallListItemStamp) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts++
	if s.attempts <= s.failures {
		return errors.New("database down")
	}
	if s.refusal != nil {
		return s.refusal
	}
	s.stamped = append(s.stamped, stamp.WorkspaceID+"|"+stamp.UserID+"|"+stamp.ItemID+"|"+stamp.CallRecordID)
	return nil
}

func runLinkedCall(t *testing.T, input OutboundCallLifecycleInput, starts *startedCalls, items callsession.CallListItems) *fakeBillingPub {
	t.Helper()
	pub := &fakeBillingPub{}
	runner := newRunner(&fakeAdmission{}, newFakeReserver(), &fakeBalanceChecker{balance: 1_000_000}, pub)
	runner.SetCDRStart(starts)
	if items != nil {
		runner.SetCallListItems(items)
	}
	call := input.Call.(*fakeCRMCall)
	call.events <- conversation.CallEvent{Type: conversation.CallEventAnswered}
	call.events <- conversation.CallEvent{Type: conversation.CallEventEnded}
	done := make(chan struct{})
	go func() {
		runner.Run(context.Background(), input)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the lifecycle never finished")
	}
	return pub
}

func linkedInput(callID string) OutboundCallLifecycleInput {
	return OutboundCallLifecycleInput{
		Call: newFakeCall(callID), WorkspaceID: "ws-1", OwnerUserID: "agent-1", StartedAt: time.Now(), PhoneTo: "5584994409684",
		Admission: &callsession.CallAdmissionLease{WorkspaceID: "ws-1", ReservedMicros: 100, PerMinuteCostMicros: 100, CallChannel: "sip"},
	}
}

func TestTheLeadAndTrunkReachTheCallRecordAndTheBill(t *testing.T) {
	starts := &startedCalls{}
	items := &stampedItems{}
	input := linkedInput("sip-out-7")
	input.LeadID, input.TrunkID, input.CallListItemID = "lead-1", "trunk-1", "item-1"

	pub := runLinkedCall(t, input, starts, items)

	if len(starts.inputs) != 1 {
		t.Fatalf("call records started = %d", len(starts.inputs))
	}
	record := starts.inputs[0]
	if record.LeadID == nil || *record.LeadID != "lead-1" || record.TrunkID == nil || *record.TrunkID != "trunk-1" {
		t.Fatalf("call record = %+v, want the lead and the trunk", record)
	}
	if ev := pub.LastEvent(t); ev.LeadID == nil || *ev.LeadID != "lead-1" {
		t.Fatalf("billing event = %+v, want the lead", ev)
	}
	if len(items.stamped) != 1 || items.stamped[0] != "ws-1|agent-1|item-1|record-1" {
		t.Fatalf("stamped = %v, want the item stamped with the call record once", items.stamped)
	}
}

func TestAFailedStampIsRetriedAndNeverStopsTheCall(t *testing.T) {
	items := &stampedItems{failures: 2}
	input := linkedInput("sip-out-11")
	input.LeadID, input.CallListItemID = "lead-1", "item-1"
	runLinkedCall(t, input, &startedCalls{}, items)
	if items.attempts != 3 || len(items.stamped) != 1 || items.stamped[0] != "ws-1|agent-1|item-1|record-1" {
		t.Fatalf("attempts %d, stamped %v, want the third attempt to stamp the call record", items.attempts, items.stamped)
	}

	items = &stampedItems{failures: 100}
	input = linkedInput("sip-out-12")
	input.LeadID, input.CallListItemID = "lead-1", "item-1"
	pub := runLinkedCall(t, input, &startedCalls{}, items)
	if items.attempts != callListStampAttempts || len(items.stamped) != 0 {
		t.Fatalf("attempts %d, stamped %v, want a bounded number of attempts", items.attempts, items.stamped)
	}
	if ev := pub.LastEvent(t); ev.LeadID == nil || *ev.LeadID != "lead-1" {
		t.Fatalf("billing event = %+v, the call still bills when the stamp fails", ev)
	}
}

func TestACallWithoutALeadLeavesTheLinksEmpty(t *testing.T) {
	starts := &startedCalls{}
	items := &stampedItems{}
	pub := runLinkedCall(t, linkedInput("wa-out-8"), starts, items)

	record := starts.inputs[0]
	if record.LeadID != nil || record.TrunkID != nil {
		t.Fatalf("call record = %+v, want no lead and no trunk", record)
	}
	if ev := pub.LastEvent(t); ev.LeadID != nil {
		t.Fatalf("billing event = %+v, want no lead", ev)
	}
	if len(items.stamped) != 0 {
		t.Fatal("no item, nothing to stamp")
	}
}

func TestAnItemIsStampedOnlyWhenTheCallRecordExists(t *testing.T) {
	starts := &startedCalls{err: errors.New("database down")}
	items := &stampedItems{}
	input := linkedInput("sip-out-9")
	input.LeadID, input.CallListItemID = "lead-1", "item-1"
	runLinkedCall(t, input, starts, items)
	if len(items.stamped) != 0 {
		t.Fatalf("stamped = %v, want nothing without a call record", items.stamped)
	}

	input = linkedInput("sip-out-10")
	input.LeadID, input.CallListItemID = "lead-1", "item-1"
	runLinkedCall(t, input, &startedCalls{}, nil)
}

func TestAStampTheItemRefusesIsNotRetried(t *testing.T) {
	items := &stampedItems{refusal: fmt.Errorf("%w: someone else holds it", callsession.ErrCallListStampRefused)}
	input := linkedInput("sip-out-13")
	input.LeadID, input.CallListItemID = "lead-1", "item-1"
	runLinkedCall(t, input, &startedCalls{}, items)
	if items.attempts != 1 || len(items.stamped) != 0 {
		t.Fatalf("attempts %d, stamped %v, want one attempt for an item that refuses the call", items.attempts, items.stamped)
	}
}
