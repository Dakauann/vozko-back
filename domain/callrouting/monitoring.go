package callrouting

import (
	"context"
	"time"
)

const ServiceLevelTarget = 20 * time.Second

type AgentState string

const (
	AgentFree    AgentState = "free"
	AgentRinging AgentState = "ringing"
	AgentOnCall  AgentState = "on_call"
	AgentWrapUp  AgentState = "wrap_up"
	AgentOffline AgentState = "offline"
)

type AgentPresence struct {
	Online    bool
	MayAnswer bool
	OnCall    bool
	Ringing   bool
}

func StateOf(presence AgentPresence, stats AgentStats, now time.Time, wrapUp time.Duration) AgentState {
	switch {
	case !presence.Online || !presence.MayAnswer:
		return AgentOffline
	case presence.OnCall:
		return AgentOnCall
	case presence.Ringing:
		return AgentRinging
	case !stats.Ready(now, wrapUp):
		return AgentWrapUp
	}
	return AgentFree
}

type AgentStatus struct {
	UserID string
	Name   string
	State  AgentState
}

type WaitingCaller struct {
	CallID       string
	RemoteNumber string
	Since        time.Time
}

type QueueLive struct {
	QueueID string
	Name    string
	Waiting []WaitingCaller
	Agents  []AgentStatus
}

func (q QueueLive) LongestWait(now time.Time) time.Duration {
	if len(q.Waiting) == 0 {
		return 0
	}
	return now.Sub(q.Waiting[0].Since)
}

func (q QueueLive) AgentCounts() map[AgentState]int {
	counts := map[AgentState]int{}
	for _, agent := range q.Agents {
		counts[agent.State]++
	}
	return counts
}

type QueueTally struct {
	QueueID              string
	Offered              int
	Answered             int
	Abandoned            int
	TimedOut             int
	AnsweredWithinTarget int
	TotalAnswerWait      time.Duration
}

func (t *QueueTally) Count(outcome TransferOutcome, wait time.Duration) {
	t.Offered++
	switch outcome {
	case OutcomeConnected:
		t.Answered++
		t.TotalAnswerWait += wait
		if wait <= ServiceLevelTarget {
			t.AnsweredWithinTarget++
		}
	case OutcomeAbandoned:
		t.Abandoned++
	case OutcomeTimedOut, OutcomeReturned:
		t.TimedOut++
	}
}

func (t QueueTally) AverageAnswerWait() time.Duration {
	if t.Answered == 0 {
		return 0
	}
	return t.TotalAnswerWait / time.Duration(t.Answered)
}

func (t QueueTally) ServiceLevel() float64 {
	if t.Offered == 0 {
		return 0
	}
	return float64(t.AnsweredWithinTarget) / float64(t.Offered)
}

type QueueOutcome struct {
	QueueID string
	Outcome TransferOutcome
	Wait    time.Duration
}

type QueueHistory interface {
	QueueOutcomes(ctx context.Context, workspaceID string, from, to time.Time) ([]QueueOutcome, error)
}
