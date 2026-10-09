package database

import "testing"

func TestEscapeLikeTreatsWildcardsAndTheEscapeLiterally(t *testing.T) {
	if got := EscapeLike(`50%_off\now`); got != `50\%\_off\\now` {
		t.Fatalf("EscapeLike = %q", got)
	}
}

func TestLikePrefixMatchesOnlyWhatStartsWithThePrefix(t *testing.T) {
	if got := LikePrefix("aichat:t_1:"); got != `aichat:t\_1:%` {
		t.Fatalf("LikePrefix = %q", got)
	}
}

func TestLikeContainsMatchesTheTextAnywhereLiterally(t *testing.T) {
	if got := LikeContains("50%_off"); got != `%50\%\_off%` {
		t.Fatalf("LikeContains = %q", got)
	}
}
