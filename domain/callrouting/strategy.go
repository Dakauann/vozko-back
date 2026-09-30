package callrouting

import (
	"math/rand/v2"
	"slices"
	"time"
)

type Strategy string

const (
	StrategyLongestIdle Strategy = "longest_idle"
	StrategyFewestCalls Strategy = "fewest_calls"
	StrategyRoundRobin  Strategy = "round_robin"
	StrategyRandom      Strategy = "random"
)

func Strategies() []Strategy {
	return []Strategy{StrategyLongestIdle, StrategyFewestCalls, StrategyRoundRobin, StrategyRandom}
}

func (s Strategy) Valid() bool {
	return slices.Contains(Strategies(), s)
}

type AgentStats struct {
	UserID          string
	LastCallEndedAt time.Time
	CallsAnswered   int
}

func (a AgentStats) Ready(now time.Time, wrapUp time.Duration) bool {
	return a.LastCallEndedAt.IsZero() || !now.Before(a.LastCallEndedAt.Add(wrapUp))
}

func RankAgents(strategy Strategy, agents []AgentStats, lastOffered string) []AgentStats {
	ranked := slices.Clone(agents)
	switch strategy {
	case StrategyFewestCalls:
		slices.SortStableFunc(ranked, func(a, b AgentStats) int {
			if a.CallsAnswered != b.CallsAnswered {
				return a.CallsAnswered - b.CallsAnswered
			}
			return a.LastCallEndedAt.Compare(b.LastCallEndedAt)
		})
	case StrategyRoundRobin:
		start := slices.IndexFunc(ranked, func(a AgentStats) bool { return a.UserID == lastOffered }) + 1
		ranked = append(ranked[start:], ranked[:start]...)
	case StrategyRandom:
		rand.Shuffle(len(ranked), func(i, j int) { ranked[i], ranked[j] = ranked[j], ranked[i] })
	default:
		slices.SortStableFunc(ranked, func(a, b AgentStats) int {
			return a.LastCallEndedAt.Compare(b.LastCallEndedAt)
		})
	}
	return ranked
}
