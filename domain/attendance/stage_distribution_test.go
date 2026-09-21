package attendance

import "testing"

func tally(funnelID, funnelName, stageID, stageName string, pos int, engaged, shell int64) StageTally {
	return StageTally{
		FunnelID:   funnelID,
		FunnelName: funnelName,
		StageID:    stageID,
		StageName:  stageName,
		Position:   pos,
		Engaged:    engaged,
		Shell:      shell,
	}
}

func TestBuildStageDistribution_SameStageNameInTwoFunnelsStaysSeparate(t *testing.T) {
	out := BuildStageDistribution([]StageTally{
		tally("f1", "FUNIL UNIFECAF", "s1", "Agendamento", 1, 300, 0),
		tally("f2", "NÃO USAR", "s2", "Agendamento", 1, 5, 0),
	}, 305, 0)

	if len(out.Funnels) != 2 {
		t.Fatalf("expected the two funnels kept apart, got %d", len(out.Funnels))
	}
	for _, f := range out.Funnels {
		if len(f.Stages) != 1 {
			t.Fatalf("funnel %q should own exactly its own stage, got %d", f.FunnelName, len(f.Stages))
		}
	}
	if out.Funnels[0].FunnelName != "FUNIL UNIFECAF" {
		t.Fatalf("expected the busiest funnel first, got %q", out.Funnels[0].FunnelName)
	}
}

func TestBuildStageDistribution_StagesKeepFunnelOrderNotVolumeOrder(t *testing.T) {
	out := BuildStageDistribution([]StageTally{
		tally("f1", "Vendas", "s3", "Matrícula", 3, 10, 0),
		tally("f1", "Vendas", "s1", "Inscrição", 1, 500, 0),
		tally("f1", "Vendas", "s2", "Documentação", 2, 200, 0),
	}, 710, 0)

	got := []string{}
	for _, s := range out.Funnels[0].Stages {
		got = append(got, s.StageName)
	}
	want := []string{"Inscrição", "Documentação", "Matrícula"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected funnel order %v, got %v", want, got)
		}
	}
}

func TestBuildStageDistribution_UnfunneledStagesBucketLast(t *testing.T) {
	out := BuildStageDistribution([]StageTally{
		tally("", "", "s9", "Solto A", 1, 900, 0),
		tally("", "", "s8", "Solto B", 2, 900, 0),
		tally("f1", "Vendas", "s1", "Inscrição", 1, 10, 0),
	}, 1810, 0)

	if len(out.Funnels) != 2 {
		t.Fatalf("expected one real funnel plus one no-funnel bucket, got %d", len(out.Funnels))
	}
	last := out.Funnels[len(out.Funnels)-1]
	if last.FunnelID != "" {
		t.Fatalf("the no-funnel bucket must sort last even when it is the biggest, got %q first-of-last", last.FunnelName)
	}
	if len(last.Stages) != 2 {
		t.Fatalf("both unfunneled stages belong to the one bucket, got %d", len(last.Stages))
	}
}

func TestBuildStageDistribution_ShellsStayOutOfTheHeadline(t *testing.T) {
	out := BuildStageDistribution([]StageTally{
		tally("f1", "Vendas", "s1", "Recebido", 1, 100, 3000),
		tally("f1", "Vendas", "s2", "Fechado", 2, 100, 0),
	}, 200, 3000)

	f := out.Funnels[0]
	if f.Engaged != 200 {
		t.Fatalf("funnel engaged must exclude shells, got %d", f.Engaged)
	}
	if f.Shell != 3000 {
		t.Fatalf("funnel shells must still be reported, got %d", f.Shell)
	}
	if f.Stages[0].PctOfFunnel != 50 {
		t.Fatalf("share is of engaged, so the import must not move it: got %v", f.Stages[0].PctOfFunnel)
	}
	if out.StagedEngaged != 200 || out.StagedShell != 3000 {
		t.Fatalf("staged totals wrong: %+v", out)
	}
}

func TestBuildStageDistribution_UnstagedIsScopedMinusStaged(t *testing.T) {
	out := BuildStageDistribution([]StageTally{
		tally("f1", "Vendas", "s1", "Inscrição", 1, 600, 20),
	}, 1000, 50)

	if out.UnstagedEngaged != 400 {
		t.Fatalf("expected 400 engaged with no stage, got %d", out.UnstagedEngaged)
	}
	if out.UnstagedShell != 30 {
		t.Fatalf("expected 30 shells with no stage, got %d", out.UnstagedShell)
	}
}

func TestBuildStageDistribution_UnstagedNeverNegative(t *testing.T) {
	out := BuildStageDistribution([]StageTally{
		tally("f1", "Vendas", "s1", "Inscrição", 1, 600, 0),
	}, 500, 0)

	if out.UnstagedEngaged != 0 {
		t.Fatalf("expected clamped 0, got %d", out.UnstagedEngaged)
	}
}

func TestBuildStageDistribution_BothSharesComputed(t *testing.T) {
	out := BuildStageDistribution([]StageTally{
		tally("f1", "A", "s1", "Um", 1, 75, 0),
		tally("f1", "A", "s2", "Dois", 2, 25, 0),
		tally("f2", "B", "s3", "Tres", 1, 100, 0),
	}, 200, 0)

	first := out.Funnels[0].Stages[0]
	if first.PctOfFunnel != 75 {
		t.Fatalf("expected 75%% of its funnel, got %v", first.PctOfFunnel)
	}
	if first.PctOfStaged != 37.5 {
		t.Fatalf("expected 37.5%% of everything staged, got %v", first.PctOfStaged)
	}
	if out.Funnels[0].PctOfStaged != 50 {
		t.Fatalf("expected the funnel to hold 50%% of staged volume, got %v", out.Funnels[0].PctOfStaged)
	}
}

func TestBuildStageDistribution_EmptyFunnelDoesNotDivideByZero(t *testing.T) {
	out := BuildStageDistribution([]StageTally{
		{FunnelID: "f1", FunnelName: "A", StageID: "s1", StageName: "Um", Shell: 4},
	}, 0, 4)

	s := out.Funnels[0].Stages[0]
	if s.PctOfFunnel != 0 || s.PctOfStaged != 0 {
		t.Fatalf("expected zero shares, got funnel=%v staged=%v", s.PctOfFunnel, s.PctOfStaged)
	}
}

func TestBuildStageDistribution_NoTalliesIsUnavailable(t *testing.T) {
	out := BuildStageDistribution(nil, 120, 3)

	if out.Available {
		t.Fatal("no staged conversation means the block is unavailable")
	}
	if out.Funnels == nil {
		t.Fatal("funnels must serialise as [] rather than null")
	}
	if out.UnstagedEngaged != 120 || out.UnstagedShell != 3 {
		t.Fatalf("everything scoped is unstaged here: %+v", out)
	}
}

func TestBuildStageDistribution_StuckRollsUpToFunnelAndBlock(t *testing.T) {
	out := BuildStageDistribution([]StageTally{
		{FunnelID: "f1", FunnelName: "A", StageID: "s1", StageName: "Um", Engaged: 10, Stuck: 3},
		{FunnelID: "f1", FunnelName: "A", StageID: "s2", StageName: "Dois", Position: 1, Engaged: 10, Stuck: 4},
		{FunnelID: "f2", FunnelName: "B", StageID: "s3", StageName: "Tres", Engaged: 1, Stuck: 1},
	}, 21, 0)

	if out.Funnels[0].Stuck != 7 {
		t.Fatalf("expected the funnel to carry 7 stuck, got %d", out.Funnels[0].Stuck)
	}
	if out.Stuck != 8 {
		t.Fatalf("expected 8 stuck across the block, got %d", out.Stuck)
	}
}

func TestEffectiveStuckDays(t *testing.T) {
	three := 3
	zero := 0
	negative := -5
	cases := []struct {
		name string
		in   *int
		want int
	}{
		{"stage defines its own threshold", &three, 3},
		{"stage defines none", nil, DefaultStageStuckDays},
		{"zero is not a threshold, it would mark everything stuck", &zero, DefaultStageStuckDays},
		{"a negative is corrupt data, not an instruction", &negative, DefaultStageStuckDays},
	}
	for _, c := range cases {
		if got := EffectiveStuckDays(c.in); got != c.want {
			t.Fatalf("%s: expected %d, got %d", c.name, c.want, got)
		}
	}
}

func TestBuildStageDistribution_StuckThresholdTravelsWithTheRow(t *testing.T) {
	twelve := 12
	out := BuildStageDistribution([]StageTally{
		{FunnelID: "f1", FunnelName: "A", StageID: "s1", StageName: "Um", RotDays: &twelve, Engaged: 5},
		{FunnelID: "f1", FunnelName: "A", StageID: "s2", StageName: "Dois", Position: 1, Engaged: 5},
	}, 10, 0)

	own := out.Funnels[0].Stages[0]
	if own.StuckAfterDays != 12 || !own.RotDaysSet {
		t.Fatalf("expected the stage's own 12-day threshold, got %d set=%v", own.StuckAfterDays, own.RotDaysSet)
	}
	fallback := out.Funnels[0].Stages[1]
	if fallback.StuckAfterDays != DefaultStageStuckDays || fallback.RotDaysSet {
		t.Fatalf("expected the default threshold flagged as a fallback, got %d set=%v",
			fallback.StuckAfterDays, fallback.RotDaysSet)
	}
}

func TestBuildStageDistribution_OutcomeFlagsSurvive(t *testing.T) {
	out := BuildStageDistribution([]StageTally{
		{FunnelID: "f1", FunnelName: "A", StageID: "s1", StageName: "Ganho", IsWon: true, Engaged: 3},
		{FunnelID: "f1", FunnelName: "A", StageID: "s2", StageName: "Perdido", Position: 1, IsLost: true, Engaged: 2},
	}, 5, 0)

	if !out.Funnels[0].Stages[0].IsWon || !out.Funnels[0].Stages[1].IsLost {
		t.Fatalf("won/lost flags lost in shaping: %+v", out.Funnels[0].Stages)
	}
}

func TestBuildStageDistribution_TiedPositionsBreakOnName(t *testing.T) {
	out := BuildStageDistribution([]StageTally{
		{FunnelID: "f1", FunnelName: "A", StageID: "s2", StageName: "Beta", Position: 1, Engaged: 1},
		{FunnelID: "f1", FunnelName: "A", StageID: "s1", StageName: "Alfa", Position: 1, Engaged: 1},
	}, 2, 0)

	if out.Funnels[0].Stages[0].StageName != "Alfa" {
		t.Fatalf("expected a stable name tiebreak, got %q", out.Funnels[0].Stages[0].StageName)
	}
}

func TestBuildStageDistribution_DwellStaysNullWhenNothingIsOpen(t *testing.T) {
	out := BuildStageDistribution([]StageTally{
		{FunnelID: "f1", FunnelName: "A", StageID: "s1", StageName: "Fechado", Engaged: 40, Finished: 40},
	}, 40, 0)

	if out.Funnels[0].Stages[0].AvgDaysInStage != nil {
		t.Fatal("a stage with no open work must report null dwell, not 0")
	}
}
