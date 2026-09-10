package comment_analysis

import "testing"

// Reputation is the SIGNED ledger per person, and it is a different quantity
// from AcceptanceScore even though both read the same stance mix.
//
// AcceptanceScore answers "how well is this account received?" — 0..100,
// unsigned, damped toward 50 while the sample is small, because a rating of an
// account has to be comparable between accounts of very different sizes.
//
// Reputation answers "what has this ONE person done to us?" — signed and
// unbounded, because four hundred supportive comments genuinely is four hundred
// times one supportive comment, and flattening that into a 0..100 would make a
// devoted supporter indistinguishable from someone who left one nice comment.
//
// Both exist. Naming them apart is not cosmetic: shown under one label they
// would read as a single number disagreeing with itself.

func TestAuthorReputationSignsTheStanceMix(t *testing.T) {
	cases := map[string]struct {
		mix  StanceMix
		high int
		want int
	}{
		// The plain reading of the ask: a positive comment earns a point, a
		// hostile one costs a point.
		"one supporter":  {StanceMix{Supporter: 1}, 0, 1},
		"one hostile":    {StanceMix{Hostile: 1}, 0, -1},
		"one critic":     {StanceMix{Critic: 1}, 0, -1}, // -0.5 rounds away from zero
		"one neutral":    {StanceMix{Neutral: 1}, 0, 0},
		"nothing at all": {StanceMix{}, 0, 0},

		// Unbounded on purpose. This is the case a 0..100 score cannot express.
		"four hundred supporters": {StanceMix{Supporter: 400}, 0, 400},

		// Critics are half a point each, so two of them cost one point. The
		// weights are the SAME constants AcceptanceScore uses; a second set
		// would let the two numbers drift apart.
		"two critics":  {StanceMix{Critic: 2}, 0, -1},
		"four critics": {StanceMix{Critic: 4}, 0, -2},

		// A mixed history nets out rather than taking the worst reading.
		"mixed nets out": {StanceMix{Supporter: 10, Neutral: 5, Critic: 2, Hostile: 1}, 0, 8},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := AuthorReputation(c.mix, c.high); got != c.want {
				t.Fatalf("AuthorReputation(%+v, %d) = %d, want %d", c.mix, c.high, got, c.want)
			}
		})
	}
}

// "aumenta conforme criticidade": a harsh comment costs more than an ordinary
// negative one. Severity is already counted per author, so the extra penalty
// rides that count rather than needing a new column.
func TestAuthorReputationChargesExtraForSeverity(t *testing.T) {
	mild := AuthorReputation(StanceMix{Hostile: 3}, 0)
	harsh := AuthorReputation(StanceMix{Hostile: 3}, 3)

	if harsh >= mild {
		t.Fatalf("three harsh hostile comments (%d) must cost more than three mild ones (%d)", harsh, mild)
	}
	if want := -6; harsh != want {
		t.Fatalf("harsh = %d, want %d (3 hostile + 3 high-severity)", harsh, want)
	}
}

// Severity cannot be charged for comments that are not in the mix. A count
// larger than the mix is a caller bug or a stale rollup, and it must not run
// the ledger away to an arbitrary number.
func TestAuthorReputationCapsSeverityAtTheMixSize(t *testing.T) {
	capped := AuthorReputation(StanceMix{Hostile: 2}, 99)
	if want := -4; capped != want {
		t.Fatalf("capped = %d, want %d (2 hostile + at most 2 severity charges)", capped, want)
	}
}

// A high-severity comment from someone whose history is otherwise supportive
// still costs, but does not erase the history. This is the case that separates
// a ledger from a flag: one bad day is visible without rewriting the person.
func TestAuthorReputationKeepsHistoryAgainstOneBadComment(t *testing.T) {
	got := AuthorReputation(StanceMix{Supporter: 20, Hostile: 1}, 1)
	if want := 18; got != want {
		t.Fatalf("got %d, want %d — a single harsh comment must not erase 20 supportive ones", got, want)
	}
}

// The two scores answer different questions and are allowed to disagree; what
// they must not do is share a scale. This pins that reputation is signed where
// acceptance is not, so a UI cannot render one with the other's formatting.
func TestReputationAndAcceptanceAreDifferentQuantities(t *testing.T) {
	mix := StanceMix{Critic: 4, Hostile: 2}

	rep := AuthorReputation(mix, 2)
	acc := AcceptanceScore(mix, 2)

	if rep >= 0 {
		t.Errorf("reputation for a hostile history = %d, want negative", rep)
	}
	if acc < 0 || acc > 100 {
		t.Errorf("acceptance = %d, want it to stay within 0..100", acc)
	}
}
