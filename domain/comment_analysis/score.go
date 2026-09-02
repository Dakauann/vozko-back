package comment_analysis

import "math"

// The acceptance score (§11.2) and an author's standing (§11.3). Both are
// pure functions of counts, never model-produced, so the number above a
// chart and the chart cannot disagree, and a customer can be told exactly
// how the figure on their slide was made.

// Stance weights: how each stance moves the mix, in [-1, 1].
const (
	StanceWeightSupporter = 1.0
	StanceWeightNeutral   = 0.0
	StanceWeightCritic    = -0.5
	StanceWeightHostile   = -1.0
)

const (
	// HighSeverityThreshold is the severity from which a comment counts as
	// "high" in stats, rollups and the score penalty.
	HighSeverityThreshold = 60

	// HighSeverityPenalty is how much a 100% high-severity share pulls the
	// score down (multiplicatively). Half: hostility already moved the mix.
	HighSeverityPenalty = 0.5

	// ConfidenceSampleSize is the analysed-comment count below which the
	// score is damped toward 50. Three glowing comments must not read 100.
	ConfidenceSampleSize = 30

	// MinCommentsForHostile is how many HOSTILE comments an author needs
	// before the history labels them hostile. One bad day is not a hater.
	MinCommentsForHostile = 3

	// FlagHighSeverityCount is the high-severity pattern that, together with
	// a hostile standing, flags an author on the dashboard.
	FlagHighSeverityCount = 3
)

// StanceMix is a count per stance.
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

// Weighted is the mean stance weight, in [-1, 1]; 0 for an empty mix.
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

// AcceptanceScore is the 0-100 the deck calls "SCORE":
//
//	weighted stance mix, normalised to 0..100
//	→ penalised by the share of comments at or above HighSeverityThreshold
//	→ damped toward 50 while the sample is below ConfidenceSampleSize
//
// highSeverityCount is the number of analysed comments with severity ≥
// HighSeverityThreshold among those in the mix.
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

// DerivedStance labels an author from their HISTORY, not from one comment.
// Same weighted mix as the score; hostile additionally requires
// MinCommentsForHostile hostile comments, otherwise the label caps at critic.
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

// IsFlagged is the dashboard's "who commented bad things" bar: a hostile
// standing AND a pattern of high-severity comments.
func IsFlagged(derived Stance, highSeverityCount int) bool {
	return derived == StanceHostile && highSeverityCount >= FlagHighSeverityCount
}
