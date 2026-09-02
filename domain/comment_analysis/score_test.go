package comment_analysis

import "testing"

// The acceptance score is the deck's "SCORE 97": pure, deterministic, never
// model-produced, and it has to survive a small sample. A number a customer
// will put in a slide must not read 100 off three comments.

func TestStanceMix_Weighted(t *testing.T) {
	cases := []struct {
		name string
		mix  StanceMix
		want float64
	}{
		{"empty is neutral", StanceMix{}, 0},
		{"all supporters", StanceMix{Supporter: 10}, 1},
		{"all hostile", StanceMix{Hostile: 10}, -1},
		{"all critics", StanceMix{Critic: 4}, -0.5},
		{"all neutral", StanceMix{Neutral: 7}, 0},
		{"balanced supporter/hostile", StanceMix{Supporter: 5, Hostile: 5}, 0},
		// (2×1 + 1×0 + 1×−0.5 + 0) / 4 = 0.375
		{"mixed", StanceMix{Supporter: 2, Neutral: 1, Critic: 1}, 0.375},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.mix.Weighted(); got != tc.want {
				t.Errorf("Weighted() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestStanceMix_Total(t *testing.T) {
	if got := (StanceMix{Supporter: 1, Neutral: 2, Critic: 3, Hostile: 4}).Total(); got != 10 {
		t.Fatalf("Total() = %d", got)
	}
}

func TestStanceMix_Add(t *testing.T) {
	var m StanceMix
	for _, s := range []Stance{StanceSupporter, StanceHostile, StanceHostile, StanceCritic, StanceNeutral, "bogus"} {
		m.Add(s)
	}
	if m != (StanceMix{Supporter: 1, Neutral: 1, Critic: 1, Hostile: 2}) {
		t.Fatalf("Add mis-counted: %+v", m)
	}
}

// THE test: three positive comments must not read 100.
func TestAcceptanceScore_SmallSampleIsDamped(t *testing.T) {
	got := AcceptanceScore(StanceMix{Supporter: 3}, 0)
	if got >= 100 {
		t.Fatalf("3 supporters scored %d; a small sample must be damped", got)
	}
	if got <= 50 {
		t.Fatalf("3 supporters scored %d; damping must not erase the signal", got)
	}
	// 50 + (100 − 50) × 3/30 = 55
	if got != 55 {
		t.Fatalf("3 supporters = %d, want 55", got)
	}
}

func TestAcceptanceScore_Extremes(t *testing.T) {
	if got := AcceptanceScore(StanceMix{}, 0); got != 50 {
		t.Errorf("no data should read 50 (unknown), got %d", got)
	}
	if got := AcceptanceScore(StanceMix{Supporter: ConfidenceSampleSize}, 0); got != 100 {
		t.Errorf("a full confident sample of supporters should read 100, got %d", got)
	}
	if got := AcceptanceScore(StanceMix{Hostile: ConfidenceSampleSize}, ConfidenceSampleSize); got != 0 {
		t.Errorf("a full confident sample of hostiles should read 0, got %d", got)
	}
	if got := AcceptanceScore(StanceMix{Neutral: 100}, 0); got != 50 {
		t.Errorf("all neutral should read 50, got %d", got)
	}
}

// More hostiles, lower score: strictly, at every step.
func TestAcceptanceScore_HostileShareIsMonotonic(t *testing.T) {
	const n = 100
	prev := 101
	for h := 0; h <= n; h += 5 {
		got := AcceptanceScore(StanceMix{Supporter: n - h, Hostile: h}, 0)
		if got >= prev {
			t.Fatalf("hostile=%d scored %d, not below the previous %d", h, got, prev)
		}
		prev = got
	}
}

// More high-severity comments, lower score, with the stance mix held fixed.
func TestAcceptanceScore_SeverityShareIsMonotonic(t *testing.T) {
	const n = 100
	mix := StanceMix{Supporter: 50, Neutral: 50}
	prev := 101
	for high := 0; high <= n; high += 10 {
		got := AcceptanceScore(mix, high)
		if got > prev {
			t.Fatalf("high=%d scored %d, above the previous %d", high, got, prev)
		}
		if high > 0 && got == prev && got != 0 {
			t.Fatalf("high=%d scored %d, no penalty applied", high, got)
		}
		prev = got
	}
}

func TestAcceptanceScore_AlwaysInRange(t *testing.T) {
	for s := 0; s <= 40; s += 10 {
		for h := 0; h <= 40; h += 10 {
			for c := 0; c <= 40; c += 20 {
				mix := StanceMix{Supporter: s, Hostile: h, Critic: c}
				for high := 0; high <= mix.Total()+5; high += 7 {
					got := AcceptanceScore(mix, high)
					if got < 0 || got > 100 {
						t.Fatalf("score %d out of range for %+v high=%d", got, mix, high)
					}
				}
			}
		}
	}
}

// ---- Author standing ----

// One bad comment does not make someone a hater. Below MinCommentsForHostile
// the worst an author can be labelled is critic. In a politically charged
// setting a product that says otherwise will be wrong about real people.
func TestDerivedStance_NeedsThreeCommentsForHostile(t *testing.T) {
	if MinCommentsForHostile != 3 {
		t.Fatalf("MinCommentsForHostile = %d; this test is named for 3", MinCommentsForHostile)
	}
	if got := DerivedStance(StanceMix{Hostile: 1}); got != StanceCritic {
		t.Errorf("one hostile comment → %q, want critic", got)
	}
	if got := DerivedStance(StanceMix{Hostile: 2}); got != StanceCritic {
		t.Errorf("two hostile comments → %q, want critic", got)
	}
	if got := DerivedStance(StanceMix{Hostile: 3}); got != StanceHostile {
		t.Errorf("three hostile comments → %q, want hostile", got)
	}
}

func TestDerivedStance(t *testing.T) {
	cases := []struct {
		name string
		mix  StanceMix
		want Stance
	}{
		{"no history is neutral", StanceMix{}, StanceNeutral},
		{"supporter", StanceMix{Supporter: 3}, StanceSupporter},
		{"mostly supporter", StanceMix{Supporter: 3, Critic: 1}, StanceSupporter}, // 0.625
		{"neutral", StanceMix{Neutral: 5}, StanceNeutral},
		{"balanced is neutral", StanceMix{Supporter: 2, Hostile: 2}, StanceNeutral},
		{"critic", StanceMix{Critic: 4}, StanceCritic},
		// −0.83 over three comments, but only TWO of them hostile: the minimum
		// counts hostile comments, not comments.
		{"critic leaning hostile under minimum", StanceMix{Hostile: 2, Critic: 1}, StanceCritic},
		{"hostile with a neutral", StanceMix{Hostile: 3, Neutral: 1}, StanceHostile},   // −0.75
		{"hostile diluted by critics", StanceMix{Hostile: 2, Critic: 2}, StanceCritic}, // −0.75, but only 2 hostile
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DerivedStance(tc.mix); got != tc.want {
				t.Errorf("DerivedStance(%+v) = %q, want %q", tc.mix, got, tc.want)
			}
		})
	}
}

// Flagging is stricter than the stance: hostile AND a pattern of high
// severity. The table the brief asks for names people; the bar is high.
func TestIsFlagged(t *testing.T) {
	if IsFlagged(StanceHostile, FlagHighSeverityCount) != true {
		t.Error("hostile with enough high-severity comments must be flagged")
	}
	if IsFlagged(StanceHostile, FlagHighSeverityCount-1) {
		t.Error("hostile without the severity pattern must not be flagged")
	}
	if IsFlagged(StanceCritic, 10) {
		t.Error("a critic is never flagged, however loud")
	}
}
