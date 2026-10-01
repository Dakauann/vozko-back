package callhistory

import (
	"testing"
	"time"

	"vozko/domain/callrouting"
	"vozko/domain/calls/cdr"
	"vozko/domain/calls/recordings"
)

var start = time.Date(2026, 9, 30, 14, 0, 0, 0, time.UTC)

func at(seconds int) time.Time { return start.Add(time.Duration(seconds) * time.Second) }

func ptr[T any](v T) *T { return &v }

func call(direction cdr.Direction, status cdr.Status, answeredAfter, endedAfter int, reason string) cdr.Call {
	c := cdr.Call{CallID: "sip-out-1", WorkspaceID: "ws1", Direction: direction, Source: cdr.SourceSIPTrunk, Status: status, StartedAt: start, AgentID: ptr("ana")}
	if answeredAfter >= 0 {
		c.AnsweredAt = ptr(at(answeredAfter))
	}
	if endedAfter >= 0 {
		c.EndedAt = ptr(at(endedAfter))
	}
	if reason != "" {
		c.EndReason = ptr(reason)
	}
	return c
}

func TestOutcomeReadsWhatHappenedToTheCall(t *testing.T) {
	cases := []struct {
		name string
		call cdr.Call
		want Outcome
	}{
		{"still running", call(cdr.DirectionOutbound, cdr.StatusInProgress, -1, -1, ""), OutcomeInProgress},
		{"answered", call(cdr.DirectionOutbound, cdr.StatusCompleted, 5, 65, "ended"), OutcomeAnswered},
		{"busy", call(cdr.DirectionOutbound, cdr.StatusFailed, -1, 8, "busy"), OutcomeBusy},
		{"declined", call(cdr.DirectionOutbound, cdr.StatusFailed, -1, 8, "declined"), OutcomeDeclined},
		{"not answered", call(cdr.DirectionOutbound, cdr.StatusFailed, -1, 60, "no_answer"), OutcomeNoAnswer},
		{"cancelled by the operator", call(cdr.DirectionOutbound, cdr.StatusAbandoned, -1, 4, "cancelled"), OutcomeCancelled},
		{"carrier failure", call(cdr.DirectionOutbound, cdr.StatusFailed, -1, 2, "failed"), OutcomeFailed},
		{"interrupted by a restart", call(cdr.DirectionOutbound, cdr.StatusAbandoned, -1, 2, cdr.EndReasonInterrupted), OutcomeFailed},
		{"inbound nobody answered", call(cdr.DirectionInbound, cdr.StatusFailed, -1, 30, "caller_hung_up"), OutcomeMissed},
		{"inbound answered", call(cdr.DirectionInbound, cdr.StatusCompleted, 3, 90, "ended"), OutcomeAnswered},
	}
	for _, tc := range cases {
		if got := OutcomeOf(tc.call); got != tc.want {
			t.Errorf("%s: outcome = %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestTalkTimeStartsAtTheAnswerAndRingTimeEndsThere(t *testing.T) {
	answered := call(cdr.DirectionOutbound, cdr.StatusCompleted, 12, 72, "ended")
	if TalkSeconds(answered) != 60 || RingSeconds(answered) != 12 {
		t.Fatalf("talk %d ring %d, want 60 and 12", TalkSeconds(answered), RingSeconds(answered))
	}
	unanswered := call(cdr.DirectionOutbound, cdr.StatusFailed, -1, 40, "no_answer")
	if TalkSeconds(unanswered) != 0 || RingSeconds(unanswered) != 40 {
		t.Fatalf("talk %d ring %d, want 0 and 40", TalkSeconds(unanswered), RingSeconds(unanswered))
	}
	running := call(cdr.DirectionOutbound, cdr.StatusInProgress, -1, -1, "")
	if TalkSeconds(running) != 0 || RingSeconds(running) != 0 {
		t.Fatal("a running call has no measured time yet")
	}
}

func TestTheChannelComesFromHowTheCallWasCarried(t *testing.T) {
	whatsApp := cdr.Call{CallID: "wa-call-1", Source: cdr.SourceWhatsApp}
	phone := cdr.Call{CallID: "sip-in-1", Source: cdr.SourceSIPTrunk}
	if ChannelOf(whatsApp) != ChannelWhatsApp || ChannelOf(phone) != ChannelPhone {
		t.Fatalf("channels = %s, %s", ChannelOf(whatsApp), ChannelOf(phone))
	}
}

func TestTheCounterpartIsWhoeverIsNotTheWorkspace(t *testing.T) {
	outbound := cdr.Call{Direction: cdr.DirectionOutbound, PhoneFrom: "", PhoneTo: "5584994409684"}
	inbound := cdr.Call{Direction: cdr.DirectionInbound, PhoneFrom: "5511999990000", PhoneTo: "551130000000"}
	if CounterpartNumber(outbound) != "5584994409684" || CounterpartNumber(inbound) != "5511999990000" {
		t.Fatalf("counterparts = %q, %q", CounterpartNumber(outbound), CounterpartNumber(inbound))
	}
}

func transfer(from, toUser, queue string, outcome callrouting.TransferOutcome, answeredBy string, created, finished int) callrouting.TransferRecord {
	kind := callrouting.TargetMember
	if queue != "" {
		kind = callrouting.TargetQueue
	}
	record := callrouting.TransferRecord{
		ID: "t-" + from + toUser + queue, CallID: "sip-out-1", FromUserID: from,
		Target:  callrouting.TransferTarget{Kind: kind, UserID: toUser, QueueID: queue},
		Outcome: outcome, AnsweredBy: answeredBy, CreatedAt: at(created),
	}
	if finished >= 0 {
		record.FinishedAt = at(finished)
	}
	return record
}

func TestHandlersFollowTheCallAcrossTransfers(t *testing.T) {
	answered := call(cdr.DirectionOutbound, cdr.StatusCompleted, 5, 300, "ended")
	transfers := []callrouting.TransferRecord{
		transfer("ana", "bia", "", callrouting.OutcomeReturned, "", 60, 80),
		transfer("ana", "", "q1", callrouting.OutcomeConnected, "caio", 100, 130),
	}
	people := ParticipantsOf(answered, transfers)
	if people.PlacedBy != "ana" || people.AnsweredBy != "caio" {
		t.Fatalf("participants = %+v", people)
	}
	if len(people.Handlers) != 2 || people.Handlers[0] != "ana" || people.Handlers[1] != "caio" {
		t.Fatalf("handlers = %v, want ana then caio", people.Handlers)
	}
	if !people.Includes("bia") || !people.Includes("caio") || people.Includes("dani") {
		t.Fatal("everyone who took part must count as a participant")
	}
}

func TestAnInboundCallIsPlacedByNobodyInTheWorkspace(t *testing.T) {
	inbound := call(cdr.DirectionInbound, cdr.StatusCompleted, 3, 90, "ended")
	inbound.AgentID = nil
	queued := []callrouting.TransferRecord{transfer("", "", "q1", callrouting.OutcomeConnected, "bia", 1, 3)}
	people := ParticipantsOf(inbound, queued)
	if people.PlacedBy != "" || people.AnsweredBy != "bia" {
		t.Fatalf("participants = %+v", people)
	}
}

func TestAnUnansweredCallHasNoHandler(t *testing.T) {
	missed := call(cdr.DirectionInbound, cdr.StatusFailed, -1, 30, "caller_hung_up")
	missed.AgentID = nil
	if people := ParticipantsOf(missed, nil); people.AnsweredBy != "" || len(people.Handlers) != 0 {
		t.Fatalf("participants = %+v", people)
	}
}

func TestTheTimelineTellsTheCallsStoryInOrder(t *testing.T) {
	answered := call(cdr.DirectionOutbound, cdr.StatusCompleted, 5, 300, "ended")
	transfers := []callrouting.TransferRecord{
		transfer("ana", "", "q1", callrouting.OutcomeConnected, "caio", 100, 130),
		transfer("ana", "bia", "", callrouting.OutcomeReturned, "", 60, 80),
	}
	transfers[1].Notes = "quer cancelar"
	recording := &recordings.CallRecord{CallID: "sip-out-1", CreatedAt: at(310), DurationSec: 295}

	timeline := BuildTimeline(answered, transfers, recording)
	want := []EntryKind{
		EntryStarted, EntryAnswered,
		EntryTransferRequested, EntryTransferReturned,
		EntryTransferRequested, EntryTransferConnected,
		EntryEnded, EntryRecordingReady,
	}
	if len(timeline) != len(want) {
		t.Fatalf("timeline = %+v", timeline)
	}
	for i, kind := range want {
		if timeline[i].Kind != kind {
			t.Fatalf("entry %d = %s, want %s (%+v)", i, timeline[i].Kind, kind, timeline)
		}
		if i > 0 && timeline[i].At.Before(timeline[i-1].At) {
			t.Fatal("the timeline must be chronological")
		}
	}
	if timeline[2].ActorID != "ana" || timeline[2].TargetUserID != "bia" || timeline[2].Notes != "quer cancelar" {
		t.Fatalf("transfer request = %+v", timeline[2])
	}
	if timeline[5].TargetQueueID != "q1" || timeline[5].ActorID != "caio" {
		t.Fatalf("queue connection = %+v", timeline[5])
	}
	if timeline[0].ActorID != "ana" || timeline[6].Reason != "ended" {
		t.Fatalf("start %+v end %+v", timeline[0], timeline[6])
	}
}

func TestATransferStillRingingHasNoOutcomeYet(t *testing.T) {
	running := call(cdr.DirectionOutbound, cdr.StatusInProgress, 5, -1, "")
	pending := transfer("ana", "bia", "", callrouting.OutcomePending, "", 60, -1)
	timeline := BuildTimeline(running, []callrouting.TransferRecord{pending}, nil)
	kinds := []EntryKind{EntryStarted, EntryAnswered, EntryTransferRequested}
	if len(timeline) != len(kinds) {
		t.Fatalf("timeline = %+v", timeline)
	}
	for i, kind := range kinds {
		if timeline[i].Kind != kind {
			t.Fatalf("entry %d = %s, want %s", i, timeline[i].Kind, kind)
		}
	}
}

func TestEveryTransferOutcomeHasItsOwnEntry(t *testing.T) {
	outcomes := map[callrouting.TransferOutcome]EntryKind{
		callrouting.OutcomeConnected: EntryTransferConnected,
		callrouting.OutcomeReturned:  EntryTransferReturned,
		callrouting.OutcomeTimedOut:  EntryTransferUnanswered,
		callrouting.OutcomeAbandoned: EntryCallerLeft,
		callrouting.OutcomeCancelled: EntryTransferCancelled,
	}
	for outcome, kind := range outcomes {
		record := transfer("ana", "bia", "", outcome, "bia", 10, 20)
		timeline := BuildTimeline(call(cdr.DirectionOutbound, cdr.StatusInProgress, 1, -1, ""), []callrouting.TransferRecord{record}, nil)
		if last := timeline[len(timeline)-1]; last.Kind != kind {
			t.Errorf("%s became %s, want %s", outcome, last.Kind, kind)
		}
	}
}
