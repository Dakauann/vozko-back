package callrouting

import (
	"testing"
	"time"
)

func TestAgentStateFollowsPresenceThenWrapUp(t *testing.T) {
	now := time.Now()
	stats := AgentStats{UserID: "ana", LastCallEndedAt: now.Add(-5 * time.Second)}
	cases := []struct {
		name     string
		presence AgentPresence
		wrapUp   time.Duration
		want     AgentState
	}{
		{"no dialer open", AgentPresence{}, 0, AgentOffline},
		{"not allowed to answer", AgentPresence{Online: true}, 0, AgentOffline},
		{"talking", AgentPresence{Online: true, MayAnswer: true, OnCall: true}, 0, AgentOnCall},
		{"being rung", AgentPresence{Online: true, MayAnswer: true, Ringing: true}, 0, AgentRinging},
		{"resting after a call", AgentPresence{Online: true, MayAnswer: true}, 10 * time.Second, AgentWrapUp},
		{"free", AgentPresence{Online: true, MayAnswer: true}, 2 * time.Second, AgentFree},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := StateOf(tc.presence, stats, now, tc.wrapUp); got != tc.want {
				t.Fatalf("state = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestTheLongestWaitIsTheFirstCallerInLine(t *testing.T) {
	now := time.Now()
	live := QueueLive{Waiting: []WaitingCaller{
		{CallID: "a", Since: now.Add(-90 * time.Second)},
		{CallID: "b", Since: now.Add(-10 * time.Second)},
	}}
	if got := live.LongestWait(now); got != 90*time.Second {
		t.Fatalf("longest wait = %v", got)
	}
	if (QueueLive{}).LongestWait(now) != 0 {
		t.Fatal("an empty queue has no wait")
	}
}

func TestLiveAgentsAreCountedByState(t *testing.T) {
	live := QueueLive{Agents: []AgentStatus{{State: AgentFree}, {State: AgentFree}, {State: AgentOnCall}, {State: AgentOffline}}}
	counts := live.AgentCounts()
	if counts[AgentFree] != 2 || counts[AgentOnCall] != 1 || counts[AgentOffline] != 1 || counts[AgentWrapUp] != 0 {
		t.Fatalf("counts = %v", counts)
	}
}

func TestTheDailyTallyReportsWaitAndServiceLevel(t *testing.T) {
	tally := QueueTally{QueueID: "q1", Offered: 10, Answered: 8, Abandoned: 1, TimedOut: 1, AnsweredWithinTarget: 6, TotalAnswerWait: 80 * time.Second}
	if got := tally.AverageAnswerWait(); got != 10*time.Second {
		t.Fatalf("average wait = %v", got)
	}
	if got := tally.ServiceLevel(); got != 0.6 {
		t.Fatalf("service level = %v", got)
	}
	empty := QueueTally{}
	if empty.AverageAnswerWait() != 0 || empty.ServiceLevel() != 0 {
		t.Fatal("an idle queue must not divide by zero")
	}
}

func TestQueueOutcomesAreSortedIntoTheTally(t *testing.T) {
	var tally QueueTally
	start := time.Now()
	tally.Count(OutcomeConnected, 12*time.Second)
	tally.Count(OutcomeConnected, 40*time.Second)
	tally.Count(OutcomeAbandoned, 30*time.Second)
	tally.Count(OutcomeTimedOut, 300*time.Second)
	tally.Count(OutcomeReturned, 300*time.Second)
	tally.Count(OutcomePending, time.Since(start))
	if tally.Offered != 6 || tally.Answered != 2 || tally.Abandoned != 1 || tally.TimedOut != 2 || tally.AnsweredWithinTarget != 1 {
		t.Fatalf("tally = %+v", tally)
	}
	if tally.TotalAnswerWait != 52*time.Second {
		t.Fatalf("answer wait = %v", tally.TotalAnswerWait)
	}
}
