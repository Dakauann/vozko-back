package audience

import "math"

const (
	StanceWeightSupporter = 1.0
	StanceWeightNeutral   = 0.0
	StanceWeightCritic    = -0.5
	StanceWeightHostile   = -1.0
)

const (
	HighSeverityThreshold = 60

	HighSeverityPenalty = 0.5

	ConfidenceSampleSize = 30

	MinCommentsForHostile = 3

	FlagHighSeverityCount = 3
)

type StanceMix struct {
	Supporter int
	Neutral   int
	Critic    int
	Hostile   int
}

func (m *StanceMix) Add(s Stance) {
	switch s {
	case StanceSupporter:
		m.Supporter++
	case StanceNeutral:
		m.Neutral++
	case StanceCritic:
		m.Critic++
	case StanceHostile:
		m.Hostile++
	}
}

func (m StanceMix) Total() int {
	return m.Supporter + m.Neutral + m.Critic + m.Hostile
}

func (m StanceMix) Weighted() float64 {
	total := m.Total()
	if total == 0 {
		return 0
	}
	sum := float64(m.Supporter)*StanceWeightSupporter +
		float64(m.Neutral)*StanceWeightNeutral +
		float64(m.Critic)*StanceWeightCritic +
		float64(m.Hostile)*StanceWeightHostile
	return sum / float64(total)
}

func AcceptanceScore(mix StanceMix, highSeverityCount int) int {
	total := mix.Total()
	raw := (mix.Weighted() + 1) / 2 * 100

	share := 0.0
	if total > 0 {
		share = math.Min(1, math.Max(0, float64(highSeverityCount)/float64(total)))
	}
	penalised := raw * (1 - HighSeverityPenalty*share)

	confidence := math.Min(1, float64(total)/ConfidenceSampleSize)
	damped := 50 + (penalised-50)*confidence

	return clampScore(int(math.Round(damped)))
}

func clampScore(s int) int {
	if s < 0 {
		return 0
	}
	if s > 100 {
		return 100
	}
	return s
}

func DerivedStance(mix StanceMix) Stance {
	if mix.Total() == 0 {
		return StanceNeutral
	}
	w := mix.Weighted()
	switch {
	case w >= 0.25:
		return StanceSupporter
	case w <= -0.75 && mix.Hostile >= MinCommentsForHostile:
		return StanceHostile
	case w <= -0.25:
		return StanceCritic
	default:
		return StanceNeutral
	}
}

func IsFlagged(derived Stance, highSeverityCount int) bool {
	return derived == StanceHostile && highSeverityCount >= FlagHighSeverityCount
}

const HighSeverityExtraPoints = -1.0

func AuthorReputation(mix StanceMix, highSeverityCount int) int {
	total := mix.Total()

	points := float64(mix.Supporter)*StanceWeightSupporter +
		float64(mix.Neutral)*StanceWeightNeutral +
		float64(mix.Critic)*StanceWeightCritic +
		float64(mix.Hostile)*StanceWeightHostile

	if highSeverityCount > total {
		highSeverityCount = total
	}
	if highSeverityCount > 0 {
		points += float64(highSeverityCount) * HighSeverityExtraPoints
	}

	if points < 0 {
		return int(math.Floor(points + 0.5 - 1e-9))
	}
	return int(math.Round(points))
}
