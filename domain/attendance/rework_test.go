package attendance

import "testing"

func TestBuildReworkSplitsHumansFromAdjacents(t *testing.T) {
	got := BuildRework([]ReworkTally{
		{ActorID: "u1", ActorKind: ActorKindHuman, DisplayName: "Bella", Finished: 364, Reopened: 40, Templates: 12, CostMicros: 900_000},
		{ActorID: "u2", ActorKind: ActorKindHuman, DisplayName: "Caio", Finished: 41, Reopened: 2},
		{ActorID: "ai", ActorKind: ActorKindAI, DisplayName: "IA", Finished: 90, Reopened: 30},
	}, ReworkTally{}, "BRL")

	if !got.Available {
		t.Fatalf("BuildRework() Available = false, want true")
	}
	if len(got.Rows) != 2 || len(got.Adjacent) != 1 {
		t.Fatalf("BuildRework() split = %d human / %d adjacent, want 2/1", len(got.Rows), len(got.Adjacent))
	}
	if got.Team.Finished != 405 || got.Team.Reopened != 42 {
		t.Fatalf("BuildRework() team = %+v, want 405 finished and 42 reopened over humans only", got.Team)
	}
	if got.Team.CostMicros != 900_000 {
		t.Fatalf("BuildRework() team cost = %d, want 900000", got.Team.CostMicros)
	}
}

func TestBuildReworkRateIsOverTheOperatorsOwnCloses(t *testing.T) {
	got := BuildRework([]ReworkTally{
		{ActorID: "u1", ActorKind: ActorKindHuman, DisplayName: "Bella", Finished: 200, Reopened: 50},
	}, ReworkTally{}, "BRL")

	row := got.Rows[0]
	if row.ReopenRate == nil || *row.ReopenRate != 25 {
		t.Fatalf("BuildRework() ReopenRate = %v, want 25", row.ReopenRate)
	}
}

func TestBuildReworkClampsReopenedToFinished(t *testing.T) {
	got := BuildRework([]ReworkTally{
		{ActorID: "u1", ActorKind: ActorKindHuman, Finished: 10, Reopened: 99},
	}, ReworkTally{}, "BRL")

	if got.Rows[0].Reopened != 10 {
		t.Fatalf("BuildRework() Reopened = %d, want it clamped to the closes", got.Rows[0].Reopened)
	}
	if got.Rows[0].ReopenRate == nil || *got.Rows[0].ReopenRate != 100 {
		t.Fatalf("BuildRework() ReopenRate = %v, want 100", got.Rows[0].ReopenRate)
	}
}

func TestBuildReworkZeroClosesHasNoRate(t *testing.T) {
	got := BuildRework([]ReworkTally{
		{ActorID: "u1", ActorKind: ActorKindHuman, Finished: 0, Reopened: 0},
	}, ReworkTally{}, "BRL")

	if got.Available {
		t.Fatalf("BuildRework() with no closes Available = true, want false")
	}
	if got.Reason != ReasonNoFinishedCloses {
		t.Fatalf("BuildRework() Reason = %q, want %q", got.Reason, ReasonNoFinishedCloses)
	}
	if got.Rows[0].ReopenRate != nil {
		t.Fatalf("BuildRework() ReopenRate = %v, want nil with a zero denominator", *got.Rows[0].ReopenRate)
	}
}

func TestBuildReworkWithoutACurrencyReportsNoCost(t *testing.T) {
	got := BuildRework([]ReworkTally{
		{ActorID: "u1", ActorKind: ActorKindHuman, Finished: 10, Reopened: 2, CostMicros: 500_000},
	}, ReworkTally{}, "")

	if !got.Available {
		t.Fatalf("BuildRework() Available = false, want true; the volume is still measured")
	}
	if got.CostAvailable {
		t.Fatalf("BuildRework() CostAvailable = true without a billing currency, want false")
	}
	if got.CostReason != ReasonNoReworkPricing {
		t.Fatalf("BuildRework() CostReason = %q, want %q", got.CostReason, ReasonNoReworkPricing)
	}
}

func TestBuildReworkKeepsUnassignedSeparate(t *testing.T) {
	got := BuildRework([]ReworkTally{
		{ActorID: "u1", ActorKind: ActorKindHuman, Finished: 10, Reopened: 1},
	}, ReworkTally{ActorKind: ActorKindHuman, Finished: 30, Reopened: 9}, "BRL")

	if got.Unassigned.Reopened != 9 {
		t.Fatalf("BuildRework() Unassigned = %+v, want the unassigned closes kept apart", got.Unassigned)
	}
	if got.Team.Finished != 10 {
		t.Fatalf("BuildRework() team absorbed the unassigned closes: %+v", got.Team)
	}
	if !got.Available {
		t.Fatalf("BuildRework() Available = false although unassigned closes exist")
	}
}

func TestBuildReworkSortsByReworkVolume(t *testing.T) {
	got := BuildRework([]ReworkTally{
		{ActorID: "u1", ActorKind: ActorKindHuman, DisplayName: "Low", Finished: 100, Reopened: 3},
		{ActorID: "u2", ActorKind: ActorKindHuman, DisplayName: "High", Finished: 100, Reopened: 30},
	}, ReworkTally{}, "BRL")

	if got.Rows[0].DisplayName != "High" {
		t.Fatalf("BuildRework() first row = %q, want the biggest rework volume first", got.Rows[0].DisplayName)
	}
}

func TestUnavailableReworkCarriesItsReason(t *testing.T) {
	got := UnavailableRework(ReasonReworkUnwatched)
	if got.Available || got.Reason != ReasonReworkUnwatched {
		t.Fatalf("UnavailableRework() = %+v, want unavailable with %q", got, ReasonReworkUnwatched)
	}
	if got.Rows == nil || got.Adjacent == nil {
		t.Fatalf("UnavailableRework() left a nil slice on the wire")
	}
}
