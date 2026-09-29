package callsession_usecase

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vozko/domain/calls/billing"
	"vozko/domain/callsession"
	"vozko/domain/conversation"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

const (
	testMinute = 100 * time.Millisecond
	testLead   = 30 * time.Millisecond
	testRetry  = 10 * time.Millisecond
	perMinute  = int64(100)
)

type scriptedReserver struct {
	*fakeInflightReserver
	mu      sync.Mutex
	outcome func(call int32) (bool, error)
}

func (s *scriptedReserver) Reserve(ws string, delta, budget int64) (bool, error) {
	call := atomic.AddInt32(&s.reserveCalls, 1)
	s.mu.Lock()
	script := s.outcome
	s.mu.Unlock()
	if script != nil {
		ok, err := script(call)
		if err != nil || !ok {
			return ok, err
		}
	}
	s.fakeInflightReserver.mu.Lock()
	defer s.fakeInflightReserver.mu.Unlock()
	if s.totals[ws]+delta > budget {
		return false, nil
	}
	s.totals[ws] += delta
	return true, nil
}

type guardedRun struct {
	call      *fakeCRMCall
	admission *fakeAdmission
	pub       *fakeBillingPub
	reason    chan string
	done      chan struct{}
	answeredC chan struct{}
}

func runGuarded(t *testing.T, runner *OutboundCallLifecycleRunner, id string, channel string) *guardedRun {
	t.Helper()
	g := &guardedRun{
		call:      newFakeCall(id),
		admission: &fakeAdmission{},
		reason:    make(chan string, 1),
		done:      make(chan struct{}),
		answeredC: make(chan struct{}),
	}
	lease := &callsession.CallAdmissionLease{WorkspaceID: "ws-1", PerMinuteCostMicros: perMinute, ReservedMicros: perMinute, CallChannel: channel}
	go func() {
		defer close(g.done)
		runner.Run(context.Background(), OutboundCallLifecycleInput{
			Call:        g.call,
			WorkspaceID: "ws-1",
			StartedAt:   time.Now(),
			Admission:   lease,
			OnStatus: func(ev conversation.CallEvent) {
				if ev.Type == conversation.CallEventAnswered {
					close(g.answeredC)
				}
			},
			OnEnded: func(reason string, _ time.Duration) { g.reason <- reason },
		})
	}()
	return g
}

func (g *guardedRun) answer(t *testing.T) time.Time {
	t.Helper()
	g.call.events <- conversation.CallEvent{Type: conversation.CallEventAnswered}
	select {
	case <-g.answeredC:
	case <-time.After(time.Second):
		t.Fatal("answer was never processed")
	}
	return time.Now()
}

func (g *guardedRun) waitEnded(t *testing.T, within time.Duration) string {
	t.Helper()
	select {
	case reason := <-g.reason:
		<-g.done
		return reason
	case <-time.After(within):
		t.Fatal("call never ended")
	}
	return ""
}

func (g *guardedRun) hangup(t *testing.T) {
	t.Helper()
	_ = g.call.Hangup()
	g.waitEnded(t, time.Second)
}

func guardedRunner(reserver *scriptedReserver, checker *fakeBalanceChecker, pub *fakeBillingPub) *OutboundCallLifecycleRunner {
	runner := newRunner(&fakeAdmission{}, nil, checker, pub)
	runner.inflightReserver = reserver
	runner.SetMinuteWaves(testMinute, testLead, testRetry)
	return runner
}

func TestAnUnansweredCallIsNeverBilledNorReservedBeyondAdmission(t *testing.T) {
	reserver := &scriptedReserver{fakeInflightReserver: newFakeReserver()}
	pub := &fakeBillingPub{}
	runner := guardedRunner(reserver, &fakeBalanceChecker{balance: 10_000}, pub)
	run := runGuarded(t, runner, "call-ringing", workspace_pricing.TelephonyChannelSIP)

	time.Sleep(3 * testMinute)
	run.call.events <- conversation.CallEvent{Type: conversation.CallEventNoAnswer}
	if reason := run.waitEnded(t, time.Second); reason != string(conversation.CallEventNoAnswer) {
		t.Fatalf("reason = %q, want no_answer", reason)
	}
	if calls := atomic.LoadInt32(&reserver.reserveCalls); calls != 0 {
		t.Fatalf("ringing reserved %d extra minutes, want 0", calls)
	}
	if pub.Count() != 0 {
		t.Fatalf("billing events for an unanswered call = %d, want 0", pub.Count())
	}
}

func TestAShortAnsweredCallBillsOneMinuteFromTheAnswerOnItsChannel(t *testing.T) {
	reserver := &scriptedReserver{fakeInflightReserver: newFakeReserver()}
	pub := &fakeBillingPub{}
	runner := guardedRunner(reserver, &fakeBalanceChecker{balance: 10_000}, pub)
	run := runGuarded(t, runner, "call-short", workspace_pricing.TelephonyChannelSIP)

	time.Sleep(50 * time.Millisecond)
	answeredAt := run.answer(t)
	run.hangup(t)

	ev := pub.LastEvent(t)
	if ev.DurationSec != 1 || billing.BilledMinutes(ev.DurationSec) != 1 {
		t.Fatalf("billable seconds = %d, want 1 (one billed minute)", ev.DurationSec)
	}
	if ev.CallStart.Before(answeredAt.Add(-10*time.Millisecond)) || ev.CallStart.After(answeredAt) {
		t.Fatalf("CallStart = %v, want the answer time %v, not the ring start", ev.CallStart, answeredAt)
	}
	if ev.Channel != workspace_pricing.TelephonyChannelSIP {
		t.Fatalf("Channel = %q, want sip so the consumer prices SIP minutes", ev.Channel)
	}
}

func TestEachNextMinuteIsReservedBeforeItStarts(t *testing.T) {
	reserver := &scriptedReserver{fakeInflightReserver: newFakeReserver()}
	pub := &fakeBillingPub{}
	runner := guardedRunner(reserver, &fakeBalanceChecker{balance: 10_000}, pub)
	admission := &fakeAdmission{}
	runner.admission = admission
	run := runGuarded(t, runner, "call-waves", workspace_pricing.TelephonyChannelSIP)

	answeredAt := run.answer(t)
	deadline := answeredAt.Add(testMinute - testLead + 20*time.Millisecond)
	for atomic.LoadInt32(&reserver.reserveCalls) < 1 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if atomic.LoadInt32(&reserver.reserveCalls) < 1 {
		t.Fatal("the second minute was not reserved before it started")
	}
	time.Sleep(2*testMinute - testLead + 10*time.Millisecond)
	run.hangup(t)

	if got := atomic.LoadInt32(&reserver.reserveCalls); got < 2 {
		t.Fatalf("reserved %d extra minutes over two minutes of talk, want >= 2", got)
	}
	if admission.lastReleased == nil || admission.lastReleased.ReservedMicros != perMinute*int64(1+atomic.LoadInt32(&reserver.reserveCalls)) {
		t.Fatalf("released reservation %+v, want every reserved minute handed back", admission.lastReleased)
	}
}

func TestACallThatCannotReserveTheNextMinuteEndsAtTheEdgeOfTheLastCoveredMinute(t *testing.T) {
	reserver := &scriptedReserver{fakeInflightReserver: newFakeReserver()}
	reserver.allowReserve = false
	reserver.outcome = func(int32) (bool, error) { return false, nil }
	pub := &fakeBillingPub{}
	runner := guardedRunner(reserver, &fakeBalanceChecker{balance: 0}, pub)
	run := runGuarded(t, runner, "call-broke", workspace_pricing.TelephonyChannelSIP)

	answeredAt := run.answer(t)
	reason := run.waitEnded(t, 2*time.Second)
	endedAfter := time.Since(answeredAt)

	if reason != endReasonInsufficientBalance {
		t.Fatalf("reason = %q, want insufficient_balance", reason)
	}
	if endedAfter < testMinute-5*time.Millisecond {
		t.Fatalf("call ended %v after answer, before the reserved minute ran out", endedAfter)
	}
	if run.call.HangupCount() == 0 {
		t.Fatal("the call was not hung up")
	}
	if pub.Count() != 1 {
		t.Fatalf("billing events = %d, want the covered minute billed", pub.Count())
	}
	if minutes := billing.BilledMinutes(pub.LastEvent(t).DurationSec); minutes != 1 {
		t.Fatalf("billed %d minutes, want the one covered minute", minutes)
	}
}

func TestConcurrentCallsShareOneBalanceAndOnlyTheUncoveredOneStops(t *testing.T) {
	shared := newFakeReserver()
	shared.totals["ws-1"] = 2 * perMinute
	reserver := &scriptedReserver{fakeInflightReserver: shared}
	pub := &fakeBillingPub{}
	runner := guardedRunner(reserver, &fakeBalanceChecker{balance: 3 * perMinute}, pub)

	first := runGuarded(t, runner, "call-a", workspace_pricing.TelephonyChannelSIP)
	second := runGuarded(t, runner, "call-b", workspace_pricing.TelephonyChannelSIP)
	first.answer(t)
	second.answer(t)

	var stopped, alive *guardedRun
	select {
	case reason := <-first.reason:
		<-first.done
		stopped, alive = first, second
		if reason != endReasonInsufficientBalance {
			t.Fatalf("stopped call reason = %q", reason)
		}
	case reason := <-second.reason:
		<-second.done
		stopped, alive = second, first
		if reason != endReasonInsufficientBalance {
			t.Fatalf("stopped call reason = %q", reason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("neither call stopped although the balance covers only one extra minute")
	}
	if stopped.call.HangupCount() == 0 {
		t.Fatal("the uncovered call was not hung up")
	}
	select {
	case <-alive.done:
		t.Fatal("the covered call was ended too")
	default:
	}
	alive.hangup(t)
}

func TestATransientBalanceErrorRecoversBeforeTheMinuteRunsOut(t *testing.T) {
	reserver := &scriptedReserver{fakeInflightReserver: newFakeReserver()}
	reserver.outcome = func(call int32) (bool, error) {
		if call == 1 {
			return false, errors.New("redis blip")
		}
		return true, nil
	}
	pub := &fakeBillingPub{}
	runner := guardedRunner(reserver, &fakeBalanceChecker{balance: 10_000}, pub)
	run := runGuarded(t, runner, "call-blip", workspace_pricing.TelephonyChannelSIP)
	run.answer(t)

	time.Sleep(testMinute + 30*time.Millisecond)
	select {
	case reason := <-run.reason:
		t.Fatalf("call ended with %q after a single transient error", reason)
	default:
	}
	run.hangup(t)
}

func TestAPersistentBalanceErrorEndsTheCallAtTheCoverageEdge(t *testing.T) {
	reserver := &scriptedReserver{fakeInflightReserver: newFakeReserver()}
	pub := &fakeBillingPub{}
	runner := guardedRunner(reserver, &fakeBalanceChecker{err: errors.New("redis down")}, pub)
	run := runGuarded(t, runner, "call-down", workspace_pricing.TelephonyChannelSIP)
	answeredAt := run.answer(t)

	if reason := run.waitEnded(t, 2*time.Second); reason != endReasonBalanceCheckError {
		t.Fatalf("reason = %q, want balance_check_error", reason)
	}
	if time.Since(answeredAt) < testMinute-5*time.Millisecond {
		t.Fatal("call ended before its reserved minute ran out")
	}
}
