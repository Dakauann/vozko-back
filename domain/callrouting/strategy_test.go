package callrouting

import (
	"testing"
	"time"
)

var epoch = time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)

func ids(agents []AgentStats) []string {
	out := make([]string, len(agents))
	for i, a := range agents {
		out[i] = a.UserID
	}
	return out
}

func sameOrder(t *testing.T, got []AgentStats, want ...string) {
	t.Helper()
	g := ids(got)
	if len(g) != len(want) {
		t.Fatalf("order = %v, want %v", g, want)
	}
	for i := range want {
		if g[i] != want[i] {
			t.Fatalf("order = %v, want %v", g, want)
		}
	}
}

func team() []AgentStats {
	return []AgentStats{
		{UserID: "ana", LastCallEndedAt: epoch.Add(-2 * time.Minute), CallsAnswered: 9},
		{UserID: "bia", LastCallEndedAt: epoch.Add(-30 * time.Minute), CallsAnswered: 4},
		{UserID: "caio", CallsAnswered: 0},
		{UserID: "davi", LastCallEndedAt: epoch.Add(-10 * time.Minute), CallsAnswered: 4},
	}
}

func TestLongestIdleOffersTheCallToWhoeverRestedLongest(t *testing.T) {
	sameOrder(t, RankAgents(StrategyLongestIdle, team(), ""), "caio", "bia", "davi", "ana")
}

func TestFewestCallsBalancesTheLoadAndBreaksTiesByIdleTime(t *testing.T) {
	sameOrder(t, RankAgents(StrategyFewestCalls, team(), ""), "caio", "bia", "davi", "ana")
}

func TestRoundRobinContinuesAfterTheLastOfferedAgent(t *testing.T) {
	sameOrder(t, RankAgents(StrategyRoundRobin, team(), "bia"), "caio", "davi", "ana", "bia")
	sameOrder(t, RankAgents(StrategyRoundRobin, team(), "davi"), "ana", "bia", "caio", "davi")
	sameOrder(t, RankAgents(StrategyRoundRobin, team(), "gone"), "ana", "bia", "caio", "davi")
}

func TestRandomKeepsEveryAgentExactlyOnce(t *testing.T) {
	got := ids(RankAgents(StrategyRandom, team(), ""))
	seen := map[string]bool{}
	for _, id := range got {
		seen[id] = true
	}
	if len(got) != 4 || len(seen) != 4 {
		t.Fatalf("random order lost or repeated agents: %v", got)
	}
}

func TestRankingNeverChangesTheCallersSlice(t *testing.T) {
	agents := team()
	RankAgents(StrategyLongestIdle, agents, "")
	sameOrder(t, agents, "ana", "bia", "caio", "davi")
}

func TestWrapUpKeepsAnAgentOutUntilItPasses(t *testing.T) {
	agent := AgentStats{UserID: "ana", LastCallEndedAt: epoch}
	if agent.Ready(epoch.Add(5*time.Second), 10*time.Second) {
		t.Fatal("agent offered a call during wrap-up")
	}
	if !agent.Ready(epoch.Add(10*time.Second), 10*time.Second) {
		t.Fatal("agent kept out after wrap-up")
	}
	if !(AgentStats{UserID: "new"}).Ready(epoch, 10*time.Second) {
		t.Fatal("an agent who never took a call must be ready")
	}
}
