package inbox_assignment

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func turnUsers(r Rotation) []string {
	out := make([]string, 0, len(r.Turns))
	for _, t := range r.Turns {
		out = append(out, t.UserID)
	}
	return out
}

func pickSequence(r Rotation, n int) []string {
	out := make([]string, 0, n)
	last := LastPick{}
	for i := 0; i < n; i++ {
		turn, _ := r.Next(last)
		out = append(out, turn.UserID)
		last = LastPick{Key: turn.Key, At: refNow.Add(time.Duration(i) * time.Minute)}
	}
	return out
}

func noDraw(int) int { return 0 }

func TestParseStrategy(t *testing.T) {
	for _, s := range Strategies() {
		got, err := ParseStrategy(string(s))
		if err != nil || got != s {
			t.Fatalf("%s: got %q, %v", s, got, err)
		}
	}
	if _, err := ParseStrategy("first_come"); !errors.Is(err, ErrInvalidStrategy) {
		t.Fatalf("an unknown strategy must be refused, got %v", err)
	}
}

func TestNormalizeStrategy_UnknownStoredValueReadsAsRoundRobin(t *testing.T) {
	if got := NormalizeStrategy(""); got != StrategyRoundRobin {
		t.Fatalf("got %q", got)
	}
	if got := NormalizeStrategy("bogus"); got != StrategyRoundRobin {
		t.Fatalf("got %q", got)
	}
	if got := NormalizeStrategy("priority"); got != StrategyPriority {
		t.Fatalf("got %q", got)
	}
}

func TestStrategyReadings(t *testing.T) {
	cases := []struct {
		s          Strategy
		recipients bool
		workload   bool
		handouts   bool
	}{
		{StrategyRoundRobin, false, false, false},
		{StrategyPriority, true, false, false},
		{StrategyWeighted, true, false, false},
		{StrategyLeastBusy, false, true, true},
		{StrategyLongestIdle, false, false, true},
		{StrategyRandom, false, false, false},
		{StrategyClaim, false, false, false},
	}
	for _, tc := range cases {
		if tc.s.ChoosesFromRecipients() != tc.recipients || tc.s.RanksByWorkload() != tc.workload || tc.s.RanksByHandouts() != tc.handouts {
			t.Fatalf("%s: readings do not match", tc.s)
		}
	}
}

func TestArrange_RoundRobinKeepsTheAvailabilityOrderAndResume(t *testing.T) {
	cands := []Candidate{{UserID: "ana"}, {UserID: "bob"}, {UserID: "cid"}}
	r := Arrange(StrategyRoundRobin, cands, ResumeAtInsertionPoint, noDraw)

	if want := []string{"ana", "bob", "cid"}; !reflect.DeepEqual(r.Order, want) || !reflect.DeepEqual(turnUsers(r), want) {
		t.Fatalf("order %v turns %v", r.Order, turnUsers(r))
	}
	if r.Turns[1].Key != "bob" {
		t.Fatalf("round robin keys are the user ids so the stored pointer keeps working, got %q", r.Turns[1].Key)
	}
	if turn, _ := r.Next(LastPick{Key: "bob"}); turn.UserID != "cid" {
		t.Fatalf("got %q", turn.UserID)
	}
	if turn, _ := r.Next(LastPick{Key: "bea"}); turn.UserID != "bob" {
		t.Fatalf("a pointer that left the ring resumes at its insertion point, got %q", turn.UserID)
	}
}

func TestArrange_RoundRobinOverLastSeenResumesAtHead(t *testing.T) {
	cands := []Candidate{{UserID: "cid"}, {UserID: "ana"}}
	r := Arrange(StrategyRoundRobin, cands, ResumeAtHead, noDraw)
	if turn, _ := r.Next(LastPick{Key: "gone"}); turn.UserID != "cid" {
		t.Fatalf("got %q", turn.UserID)
	}
}

func TestArrange_PriorityRotatesInsideTheBestTier(t *testing.T) {
	cands := []Candidate{
		{UserID: "zed", Tier: 2},
		{UserID: "bob", Tier: 1},
		{UserID: "ana", Tier: 1},
		{UserID: "cid", Tier: 3},
	}
	r := Arrange(StrategyPriority, cands, ResumeAtHead, noDraw)

	if want := []string{"ana", "bob", "zed", "cid"}; !reflect.DeepEqual(r.Order, want) {
		t.Fatalf("preference order %v want %v", r.Order, want)
	}
	if want := []string{"ana", "bob"}; !reflect.DeepEqual(turnUsers(r), want) {
		t.Fatalf("only the best tier takes turns, got %v", turnUsers(r))
	}
	if got := pickSequence(r, 4); !reflect.DeepEqual(got, []string{"ana", "bob", "ana", "bob"}) {
		t.Fatalf("got %v", got)
	}
}

func TestArrange_PriorityOverflowsWhenTheTopTierIsGone(t *testing.T) {
	r := Arrange(StrategyPriority, []Candidate{{UserID: "zed", Tier: 2}, {UserID: "yan", Tier: 2}, {UserID: "cid", Tier: 3}}, ResumeAtHead, noDraw)
	if want := []string{"yan", "zed"}; !reflect.DeepEqual(turnUsers(r), want) {
		t.Fatalf("got %v", turnUsers(r))
	}
}

func TestArrange_PriorityWithAPointerFromAnotherTierResumesAtItsInsertionPoint(t *testing.T) {
	r := Arrange(StrategyPriority, []Candidate{{UserID: "ana", Tier: 1}, {UserID: "cid", Tier: 1}}, ResumeAtHead, noDraw)
	if turn, _ := r.Next(LastPick{Key: "bob"}); turn.UserID != "cid" {
		t.Fatalf("got %q", turn.UserID)
	}
}

func TestArrange_PriorityNeverPicksSomeoneWithoutATier(t *testing.T) {
	r := Arrange(StrategyPriority, []Candidate{{UserID: "ana"}, {UserID: "bob", Tier: 0}}, ResumeAtHead, noDraw)
	if r.CanPick() || len(r.Order) != 0 {
		t.Fatalf("candidates outside the recipient list must never receive, got %+v", r)
	}
}

func TestArrange_WeightedSharesInProportionWithoutBursts(t *testing.T) {
	r := Arrange(StrategyWeighted, []Candidate{{UserID: "bob", Weight: 1}, {UserID: "ana", Weight: 3}, {UserID: "cid", Weight: 2}}, ResumeAtHead, noDraw)

	if want := []string{"ana", "cid", "bob"}; !reflect.DeepEqual(r.Order, want) {
		t.Fatalf("preference order %v want %v", r.Order, want)
	}
	if want := []string{"ana", "cid", "ana", "bob", "cid", "ana"}; !reflect.DeepEqual(turnUsers(r), want) {
		t.Fatalf("smooth sequence %v want %v", turnUsers(r), want)
	}
	counts := map[string]int{}
	for _, u := range pickSequence(r, 60) {
		counts[u]++
	}
	if counts["ana"] != 30 || counts["cid"] != 20 || counts["bob"] != 10 {
		t.Fatalf("shares %v", counts)
	}
}

func TestArrange_WeightedKeysAreUniquePerSlot(t *testing.T) {
	r := Arrange(StrategyWeighted, []Candidate{{UserID: "ana", Weight: 2}, {UserID: "bob", Weight: 1}}, ResumeAtHead, noDraw)
	seen := map[string]bool{}
	for _, turn := range r.Turns {
		if seen[turn.Key] {
			t.Fatalf("duplicate key %q", turn.Key)
		}
		seen[turn.Key] = true
	}
	if turn, _ := r.Next(LastPick{Key: "gone#4"}); turn.UserID != "ana" {
		t.Fatalf("a slot that disappeared resumes at the head, got %q", turn.UserID)
	}
}

func TestArrange_WeightedNeverPicksSomeoneWithoutAWeight(t *testing.T) {
	r := Arrange(StrategyWeighted, []Candidate{{UserID: "ana"}}, ResumeAtHead, noDraw)
	if r.CanPick() {
		t.Fatalf("got %+v", r)
	}
}

func TestArrange_LeastBusyPicksTheFewestOpenAndBreaksTiesByIdleTime(t *testing.T) {
	cands := []Candidate{
		{UserID: "ana", OpenConversations: 4},
		{UserID: "bob", OpenConversations: 1, LastHandout: ago(time.Minute)},
		{UserID: "cid", OpenConversations: 1, LastHandout: ago(time.Hour)},
		{UserID: "dan", OpenConversations: 1, LastHandout: ago(time.Hour)},
	}
	r := Arrange(StrategyLeastBusy, cands, ResumeAtHead, noDraw)

	if want := []string{"cid", "dan", "bob", "ana"}; !reflect.DeepEqual(r.Order, want) {
		t.Fatalf("got %v want %v", r.Order, want)
	}
	if turn, _ := r.Next(LastPick{}); turn.UserID != "cid" {
		t.Fatalf("got %q", turn.UserID)
	}
}

func TestArrange_LeastBusyFoldsThePointerInAsTheFreshestHandout(t *testing.T) {
	cands := []Candidate{
		{UserID: "ana", OpenConversations: 0, LastHandout: ago(time.Hour)},
		{UserID: "bob", OpenConversations: 0, LastHandout: ago(30 * time.Minute)},
	}
	r := Arrange(StrategyLeastBusy, cands, ResumeAtHead, noDraw)
	if turn, _ := r.Next(LastPick{Key: "ana", At: refNow}); turn.UserID != "bob" {
		t.Fatalf("ana was just picked and ties on load, got %q", turn.UserID)
	}
}

func TestArrange_LeastBusyStillPicksTheLastOneWhenTheyAreClearlyLessBusy(t *testing.T) {
	cands := []Candidate{
		{UserID: "ana", OpenConversations: 0},
		{UserID: "bob", OpenConversations: 5},
	}
	r := Arrange(StrategyLeastBusy, cands, ResumeAtHead, noDraw)
	if turn, _ := r.Next(LastPick{Key: "ana", At: refNow}); turn.UserID != "ana" {
		t.Fatalf("got %q", turn.UserID)
	}
}

func TestArrange_LongestIdlePutsNeverServedFirst(t *testing.T) {
	cands := []Candidate{
		{UserID: "ana", LastHandout: ago(time.Minute)},
		{UserID: "dan"},
		{UserID: "bob", LastHandout: ago(3 * time.Hour)},
		{UserID: "cid"},
	}
	r := Arrange(StrategyLongestIdle, cands, ResumeAtHead, noDraw)
	if want := []string{"cid", "dan", "bob", "ana"}; !reflect.DeepEqual(r.Order, want) {
		t.Fatalf("got %v want %v", r.Order, want)
	}
}

func TestArrange_LongestIdleDoesNotRepeatAPickWhoseHandoutIsNotVisibleYet(t *testing.T) {
	cands := []Candidate{
		{UserID: "ana", LastHandout: ago(5 * time.Hour)},
		{UserID: "bob", LastHandout: ago(time.Hour)},
	}
	r := Arrange(StrategyLongestIdle, cands, ResumeAtHead, noDraw)
	if turn, _ := r.Next(LastPick{Key: "ana", At: refNow}); turn.UserID != "bob" {
		t.Fatalf("got %q", turn.UserID)
	}
	if turn, _ := r.Next(LastPick{Key: "ana", At: ago(6 * time.Hour)}); turn.UserID != "ana" {
		t.Fatalf("an older pointer than the recorded hand-out changes nothing, got %q", turn.UserID)
	}
}

func TestArrange_RandomPicksTheDrawnCandidate(t *testing.T) {
	cands := []Candidate{{UserID: "ana"}, {UserID: "bob"}, {UserID: "cid"}}
	draws := []int{2, 0}
	draw := func(n int) int {
		d := draws[0]
		draws = draws[1:]
		return d % n
	}
	r := Arrange(StrategyRandom, cands, ResumeAtHead, draw)
	if turn, _ := r.Next(LastPick{Key: "cid"}); turn.UserID != "cid" {
		t.Fatalf("random is uniform and may repeat, got %q", turn.UserID)
	}
	if want := []string{"cid", "ana", "bob"}; !reflect.DeepEqual(r.Order, want) {
		t.Fatalf("got %v want %v", r.Order, want)
	}
}

func TestArrange_ClaimKeepsTheAvailableButPicksNobody(t *testing.T) {
	r := Arrange(StrategyClaim, []Candidate{{UserID: "ana"}, {UserID: "bob"}}, ResumeAtInsertionPoint, noDraw)
	if r.CanPick() {
		t.Fatal("claim must not pick")
	}
	if want := []string{"ana", "bob"}; !reflect.DeepEqual(r.Order, want) {
		t.Fatalf("the available stay listed for outreach stickiness, got %v", r.Order)
	}
}

func TestArrange_EmptyCandidatesPickNobody(t *testing.T) {
	for _, s := range Strategies() {
		r := Arrange(s, nil, ResumeAtHead, noDraw)
		if r.CanPick() || len(r.Order) != 0 {
			t.Fatalf("%s: got %+v", s, r)
		}
		if turn, _ := r.Next(LastPick{Key: "ana"}); turn.UserID != "" {
			t.Fatalf("%s: got %q", s, turn.UserID)
		}
	}
}

func TestArrange_DropsBlankAndDuplicateCandidates(t *testing.T) {
	r := Arrange(StrategyRoundRobin, []Candidate{{UserID: "ana"}, {UserID: ""}, {UserID: "ana"}, {UserID: "bob"}}, ResumeAtHead, noDraw)
	if want := []string{"ana", "bob"}; !reflect.DeepEqual(r.Order, want) {
		t.Fatalf("got %v", r.Order)
	}
}

func TestWithinCapacity(t *testing.T) {
	cands := []Candidate{{UserID: "ana", OpenConversations: 3}, {UserID: "bob", OpenConversations: 2}, {UserID: "cid"}}
	if got := WithinCapacity(cands, 0); len(got) != 3 {
		t.Fatalf("no limit keeps everyone, got %v", got)
	}
	got := WithinCapacity(cands, 3)
	if len(got) != 2 || got[0].UserID != "bob" || got[1].UserID != "cid" {
		t.Fatalf("a member holding the limit is full, got %v", got)
	}
	if got := WithinCapacity(cands, 1); len(got) != 1 || got[0].UserID != "cid" {
		t.Fatalf("got %v", got)
	}
}

func TestOnlyRecipients_KeepsTheAvailabilityOrderAndAttachesTierAndWeight(t *testing.T) {
	cands := []Candidate{{UserID: "cid"}, {UserID: "ana"}, {UserID: "bob"}}
	got := OnlyRecipients(cands, []Recipient{{UserID: "ana", Tier: 2, Weight: 5}, {UserID: "cid", Tier: 1, Weight: 1}, {UserID: "zed", Tier: 1, Weight: 1}})

	want := []Candidate{{UserID: "cid", Tier: 1, Weight: 1}, {UserID: "ana", Tier: 2, Weight: 5}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
	if got := OnlyRecipients(cands, nil); len(got) != 0 {
		t.Fatalf("an empty list lets nobody through, got %v", got)
	}
}
