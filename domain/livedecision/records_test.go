package livedecision

import (
	"testing"
	"time"

	"vozko/domain/audience"
	"vozko/domain/decision"
)

func TestALiveReadCarriesTheLabelsAndTheWatermark(t *testing.T) {
	outcome, err := Interpret(snapshot(), Features{Analysis: true}, answers(nil), decision.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	read, ok := NewLiveRead(snapshot(), outcome, t0.Add(time.Hour))
	if !ok {
		t.Fatal("an outcome with a reading makes a live read")
	}
	if read.Qualification != audience.QualificationHotLead || read.AttendanceQuality == 0 || read.Language != "pt" {
		t.Fatalf("read = %+v", read)
	}
	if !read.DecidedThrough.Equal(t0.Add(time.Minute)) || !read.DecidedAt.Equal(t0.Add(time.Hour)) {
		t.Fatalf("watermark = %v, decided = %v", read.DecidedThrough, read.DecidedAt)
	}
	if read.Certainty[audience.FieldQualification] != 0.97 {
		t.Fatalf("certainty = %v", read.Certainty)
	}
}

func TestTheStageDecisionIsRememberedEvenWithoutAnalysis(t *testing.T) {
	read, ok := NewLiveRead(snapshot(), Outcome{StageUncertain: true}, t0)
	if !ok || read.StageSettled || read.HasLabels() {
		t.Fatalf("an unsure stage is recorded as unsettled: read = %+v, ok = %v", read, ok)
	}
	read, ok = NewLiveRead(snapshot(), Outcome{StageSettled: true}, t0)
	if !ok || !read.StageSettled {
		t.Fatalf("a confident stage decision is recorded as settled: read = %+v, ok = %v", read, ok)
	}
	if _, ok := NewLiveRead(snapshot(), Outcome{}, t0); ok {
		t.Fatal("an outcome with nothing to read makes no live read")
	}
}

func TestOnlyANewerReadReplacesAnOlderOne(t *testing.T) {
	older := LiveRead{DecidedThrough: t0}
	newer := LiveRead{DecidedThrough: t0.Add(time.Second)}
	if !newer.Supersedes(older) || older.Supersedes(newer) {
		t.Fatal("the later watermark wins")
	}
	if !older.Supersedes(older) {
		t.Fatal("a re-decision of the same message replaces it")
	}
}

func TestEffectsAreNamedForTheLog(t *testing.T) {
	if effects := (Outcome{MoveStageTo: "s"}).Effects(); len(effects) != 1 || effects[0] != EffectStageMoved {
		t.Fatalf("effects = %v", effects)
	}
	if effects := (Outcome{StageUncertain: true}).Effects(); len(effects) != 1 || effects[0] != EffectStageUncertain {
		t.Fatalf("effects = %v", effects)
	}
	if effects := (Outcome{StageSettled: true}).Effects(); len(effects) != 0 {
		t.Fatalf("keeping the stage has no effect: %v", effects)
	}
}

func TestAReadWithoutLabelsHasNoView(t *testing.T) {
	if (LiveRead{StageSettled: true}).View() != nil {
		t.Fatal("a stage-only read shows nothing on the card")
	}
	view := LiveRead{Qualification: audience.QualificationHotLead, AttendanceQuality: 80, DecidedAt: t0}.View()
	if view == nil || view.Qualification != "hot_lead" || view.AttendanceQuality != 80 || !view.DecidedAt.Equal(t0) {
		t.Fatalf("view = %+v", view)
	}
}
