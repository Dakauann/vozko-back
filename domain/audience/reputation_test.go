package audience

import "testing"

func TestAuthorReputationSignsTheStanceMix(t *testing.T) {
	cases := map[string]struct {
		mix  StanceMix
		high int
		want int
	}{
		"one supporter":  {StanceMix{Supporter: 1}, 0, 1},
		"one hostile":    {StanceMix{Hostile: 1}, 0, -1},
		"one critic":     {StanceMix{Critic: 1}, 0, -1},
		"one neutral":    {StanceMix{Neutral: 1}, 0, 0},
		"nothing at all": {StanceMix{}, 0, 0},

		"four hundred supporters": {StanceMix{Supporter: 400}, 0, 400},

		"two critics":  {StanceMix{Critic: 2}, 0, -1},
		"four critics": {StanceMix{Critic: 4}, 0, -2},

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

func TestAuthorReputationCapsSeverityAtTheMixSize(t *testing.T) {
	capped := AuthorReputation(StanceMix{Hostile: 2}, 99)
	if want := -4; capped != want {
		t.Fatalf("capped = %d, want %d (2 hostile + at most 2 severity charges)", capped, want)
	}
}

func TestAuthorReputationKeepsHistoryAgainstOneBadComment(t *testing.T) {
	got := AuthorReputation(StanceMix{Supporter: 20, Hostile: 1}, 1)
	if want := 18; got != want {
		t.Fatalf("got %d, want %d — a single harsh comment must not erase 20 supportive ones", got, want)
	}
}

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
