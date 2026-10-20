package inbox_assignment

import (
	"cmp"
	"slices"
	"strconv"
	"time"
)

type Strategy string

const (
	StrategyRoundRobin  Strategy = "round_robin"
	StrategyPriority    Strategy = "priority"
	StrategyWeighted    Strategy = "weighted"
	StrategyLeastBusy   Strategy = "least_busy"
	StrategyLongestIdle Strategy = "longest_idle"
	StrategyRandom      Strategy = "random"
	StrategyClaim       Strategy = "claim"

	DefaultStrategy = StrategyRoundRobin
)

func Strategies() []Strategy {
	return []Strategy{StrategyRoundRobin, StrategyPriority, StrategyWeighted, StrategyLeastBusy, StrategyLongestIdle, StrategyRandom, StrategyClaim}
}

func (s Strategy) Valid() bool { return slices.Contains(Strategies(), s) }

func ParseStrategy(raw string) (Strategy, error) {
	s := Strategy(raw)
	if !s.Valid() {
		return "", ErrInvalidStrategy
	}
	return s, nil
}

func NormalizeStrategy(raw string) Strategy {
	if s := Strategy(raw); s.Valid() {
		return s
	}
	return DefaultStrategy
}

func (s Strategy) ChoosesFromRecipients() bool {
	return s == StrategyPriority || s == StrategyWeighted
}

func (s Strategy) RanksByWorkload() bool { return s == StrategyLeastBusy }

func (s Strategy) RanksByHandouts() bool {
	return s == StrategyLeastBusy || s == StrategyLongestIdle
}

type PickRule int

const (
	PickNextTurn PickRule = iota
	PickRanked
	PickFirst
)

type Turn struct {
	Key    string
	UserID string
}

type LastPick struct {
	Key string
	At  time.Time
}

type Rotation struct {
	Order  []string
	Turns  []Turn
	Resume ResumePolicy
	Pick   PickRule

	ranking Strategy
	ranked  []Candidate
}

func (r Rotation) CanPick() bool { return len(r.Turns) > 0 }

func (r Rotation) Next(last LastPick) (Turn, int) {
	if !r.CanPick() {
		return Turn{}, 0
	}
	switch r.Pick {
	case PickRanked:
		return r.nextRanked(last)
	case PickFirst:
		return r.Turns[0], 0
	default:
		idx := NextIndex(turnKeys(r.Turns), last.Key, r.Resume)
		return r.Turns[idx], idx
	}
}

func (r Rotation) nextRanked(last LastPick) (Turn, int) {
	cands := slices.Clone(r.ranked)
	for i := range cands {
		if cands[i].UserID == last.Key && last.At.After(cands[i].LastHandout) {
			cands[i].LastHandout = last.At
		}
	}
	rank(r.ranking, cands)
	return Turn{Key: cands[0].UserID, UserID: cands[0].UserID}, 0
}

func turnKeys(turns []Turn) []string {
	keys := make([]string, len(turns))
	for i, t := range turns {
		keys[i] = t.Key
	}
	return keys
}

func Arrange(strategy Strategy, cands []Candidate, resume ResumePolicy, draw func(n int) int) Rotation {
	cands = distinct(cands)
	switch strategy {
	case StrategyPriority:
		return arrangePriority(cands)
	case StrategyWeighted:
		return arrangeWeighted(cands)
	case StrategyLeastBusy, StrategyLongestIdle:
		return arrangeRanked(strategy, cands)
	case StrategyRandom:
		return arrangeRandom(cands, draw)
	case StrategyClaim:
		return Rotation{Order: CandidateIDs(cands)}
	default:
		order := CandidateIDs(cands)
		return Rotation{Order: order, Turns: selfTurns(order), Resume: resume, Pick: PickNextTurn}
	}
}

func arrangePriority(cands []Candidate) Rotation {
	tiered := slices.DeleteFunc(slices.Clone(cands), func(c Candidate) bool { return c.Tier < MinRecipientTier })
	slices.SortFunc(tiered, func(a, b Candidate) int {
		return cmp.Or(cmp.Compare(a.Tier, b.Tier), cmp.Compare(a.UserID, b.UserID))
	})
	order := CandidateIDs(tiered)
	if len(tiered) == 0 {
		return Rotation{Order: order}
	}
	best := tiered[0].Tier
	top := slices.IndexFunc(tiered, func(c Candidate) bool { return c.Tier != best })
	if top < 0 {
		top = len(tiered)
	}
	return Rotation{Order: order, Turns: selfTurns(order[:top]), Resume: ResumeAtInsertionPoint, Pick: PickNextTurn}
}

func arrangeWeighted(cands []Candidate) Rotation {
	weighted := slices.DeleteFunc(slices.Clone(cands), func(c Candidate) bool { return c.Weight < MinRecipientWeight })
	slices.SortFunc(weighted, func(a, b Candidate) int { return cmp.Compare(a.UserID, b.UserID) })

	preference := slices.Clone(weighted)
	slices.SortStableFunc(preference, func(a, b Candidate) int { return cmp.Compare(b.Weight, a.Weight) })

	return Rotation{Order: CandidateIDs(preference), Turns: smoothWeightedTurns(weighted), Resume: ResumeAtHead, Pick: PickNextTurn}
}

func smoothWeightedTurns(cands []Candidate) []Turn {
	total := 0
	for _, c := range cands {
		total += c.Weight
	}
	current := make([]int, len(cands))
	served := make([]int, len(cands))
	turns := make([]Turn, 0, total)
	for range total {
		chosen := 0
		for i, c := range cands {
			current[i] += c.Weight
			if current[i] > current[chosen] {
				chosen = i
			}
		}
		current[chosen] -= total
		served[chosen]++
		userID := cands[chosen].UserID
		turns = append(turns, Turn{Key: userID + "#" + strconv.Itoa(served[chosen]), UserID: userID})
	}
	return turns
}

func arrangeRanked(strategy Strategy, cands []Candidate) Rotation {
	ranked := slices.Clone(cands)
	rank(strategy, ranked)
	order := CandidateIDs(ranked)
	return Rotation{Order: order, Turns: selfTurns(order), Pick: PickRanked, ranking: strategy, ranked: ranked}
}

func rank(strategy Strategy, cands []Candidate) {
	slices.SortFunc(cands, func(a, b Candidate) int {
		byIdle := cmp.Or(a.LastHandout.Compare(b.LastHandout), cmp.Compare(a.UserID, b.UserID))
		if strategy == StrategyLeastBusy {
			return cmp.Or(cmp.Compare(a.OpenConversations, b.OpenConversations), byIdle)
		}
		return byIdle
	})
}

func arrangeRandom(cands []Candidate, draw func(n int) int) Rotation {
	pool := CandidateIDs(cands)
	order := make([]string, 0, len(pool))
	for len(pool) > 1 {
		i := draw(len(pool))
		order = append(order, pool[i])
		pool = slices.Delete(pool, i, i+1)
	}
	order = append(order, pool...)
	return Rotation{Order: order, Turns: selfTurns(order), Pick: PickFirst}
}

func WithinCapacity(cands []Candidate, maxOpen int) []Candidate {
	if maxOpen <= 0 {
		return cands
	}
	return slices.DeleteFunc(slices.Clone(cands), func(c Candidate) bool { return c.OpenConversations >= maxOpen })
}

func OnlyRecipients(cands []Candidate, recipients []Recipient) []Candidate {
	byUser := make(map[string]Recipient, len(recipients))
	for _, r := range recipients {
		byUser[r.UserID] = r
	}
	out := make([]Candidate, 0, len(recipients))
	for _, c := range cands {
		r, listed := byUser[c.UserID]
		if !listed {
			continue
		}
		c.Tier, c.Weight = r.Tier, r.Weight
		out = append(out, c)
	}
	return out
}

func distinct(cands []Candidate) []Candidate {
	seen := make(map[string]bool, len(cands))
	out := make([]Candidate, 0, len(cands))
	for _, c := range cands {
		if c.UserID == "" || seen[c.UserID] {
			continue
		}
		seen[c.UserID] = true
		out = append(out, c)
	}
	return out
}

func CandidateIDs(cands []Candidate) []string {
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.UserID
	}
	return out
}

func selfTurns(userIDs []string) []Turn {
	turns := make([]Turn, len(userIDs))
	for i, id := range userIDs {
		turns[i] = Turn{Key: id, UserID: id}
	}
	return turns
}
