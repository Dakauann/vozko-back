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

// The whole reason this block is grouped: two funnels in one workspace can each
// own a stage called "Agendamento", and five production workspaces really do
// carry more than one conversation funnel. Flattening by name would report a
// number belonging to neither funnel.
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
	// Busiest funnel first: the panel's first row must be where the work is.
	if out.Funnels[0].FunnelName != "FUNIL UNIFECAF" {
		t.Fatalf("expected the busiest funnel first, got %q", out.Funnels[0].FunnelName)
	}
}

// Inside a funnel the order is the funnel's own, not a ranking. A pipeline read
// out of order stops being a pipeline, and "where do they stop moving" is only
// answerable when the stages are in path order.
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

// A stage with no pipeline is a real row in this product (campaign-scoped stages
// predate funnels). It gets one bucket, and that bucket sorts last no matter how
// big it is, because it is not a funnel and must not head the panel.
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

// Shells are shown, never summed into the headline. A 3.000-contact import
// parked in "Recebido" would otherwise dwarf every other stage on the chart and
// contradict the engaged-only KPI strip above the panel.
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

// Conversations carrying no stage row at all are a total, not a fake stage. They
// are the difference between what the period scoped and what the funnels hold,
// and a manager reading "612 in Inscrição" needs to know 400 are nowhere.
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

// Never negative. If a stage row somehow outnumbers the scope it was counted in
// (a stage assigned between the two reads), the honest answer is zero unstaged,
// not a negative count rendered on a chart.
func TestBuildStageDistribution_UnstagedNeverNegative(t *testing.T) {
	out := BuildStageDistribution([]StageTally{
		tally("f1", "Vendas", "s1", "Inscrição", 1, 600, 0),
	}, 500, 0)

	if out.UnstagedEngaged != 0 {
		t.Fatalf("expected clamped 0, got %d", out.UnstagedEngaged)
	}
}

// Percentages answer two different questions and both are on screen: share of
// this funnel (where inside the path) and share of everything staged (which
// funnel owns the workspace's volume).
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

// A funnel holding zero engaged conversations must not divide by zero, and its
// stage shares are 0, not NaN, which serialises as null and breaks the chart.
func TestBuildStageDistribution_EmptyFunnelDoesNotDivideByZero(t *testing.T) {
	out := BuildStageDistribution([]StageTally{
		{FunnelID: "f1", FunnelName: "A", StageID: "s1", StageName: "Um", Shell: 4},
	}, 0, 4)

	s := out.Funnels[0].Stages[0]
	if s.PctOfFunnel != 0 || s.PctOfStaged != 0 {
		t.Fatalf("expected zero shares, got funnel=%v staged=%v", s.PctOfFunnel, s.PctOfStaged)
	}
}

// Nothing staged in the period is not an error and not an empty chart with a
// zero axis: the block reports itself unavailable so the UI can say why.
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

// Stuck rolls up. The funnel header carries the sum of its stages so a manager
// scanning collapsed funnels sees which one is holding stalled work.
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

// The stage's own rot_days wins; the CRM's 7-day default only fills the gap. A
// conversation must not read "parada" on the kanban and fresh here.
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

// The threshold that produced the stuck count travels with the row, so the UI
// can name it instead of leaving the reader to guess what "stuck" measured.
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

// Won and lost stages are the funnel's outcomes, and the panel colours them as
// outcomes rather than as more volume. The flags have to survive the shaping.
func TestBuildStageDistribution_OutcomeFlagsSurvive(t *testing.T) {
	out := BuildStageDistribution([]StageTally{
		{FunnelID: "f1", FunnelName: "A", StageID: "s1", StageName: "Ganho", IsWon: true, Engaged: 3},
		{FunnelID: "f1", FunnelName: "A", StageID: "s2", StageName: "Perdido", Position: 1, IsLost: true, Engaged: 2},
	}, 5, 0)

	if !out.Funnels[0].Stages[0].IsWon || !out.Funnels[0].Stages[1].IsLost {
		t.Fatalf("won/lost flags lost in shaping: %+v", out.Funnels[0].Stages)
	}
}

// Two stages at the same position is ordinary after a drag-reorder that half
// applied. Name breaks the tie so the panel is stable between refreshes rather
// than shuffling rows on every load.
func TestBuildStageDistribution_TiedPositionsBreakOnName(t *testing.T) {
	out := BuildStageDistribution([]StageTally{
		{FunnelID: "f1", FunnelName: "A", StageID: "s2", StageName: "Beta", Position: 1, Engaged: 1},
		{FunnelID: "f1", FunnelName: "A", StageID: "s1", StageName: "Alfa", Position: 1, Engaged: 1},
	}, 2, 0)

	if out.Funnels[0].Stages[0].StageName != "Alfa" {
		t.Fatalf("expected a stable name tiebreak, got %q", out.Funnels[0].Stages[0].StageName)
	}
}

// Dwell is a measurement, not a count: a stage holding only finished work has no
// open conversation to measure, and null is the honest answer. Zero would read
// as "everyone here arrived today".
func TestBuildStageDistribution_DwellStaysNullWhenNothingIsOpen(t *testing.T) {
	out := BuildStageDistribution([]StageTally{
		{FunnelID: "f1", FunnelName: "A", StageID: "s1", StageName: "Fechado", Engaged: 40, Finished: 40},
	}, 40, 0)

	if out.Funnels[0].Stages[0].AvgDaysInStage != nil {
		t.Fatal("a stage with no open work must report null dwell, not 0")
	}
}
