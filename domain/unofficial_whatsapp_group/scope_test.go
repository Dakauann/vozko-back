package unofficial_whatsapp_group

import (
	"testing"
	"time"

	uw "vozko/domain/unofficial_whatsapp"
)

func TestScopeOfKeepsOnlyNumbersTheDepartmentScopeAllows(t *testing.T) {
	sales, support := "sales", "support"
	instances := []*uw.Instance{
		{ID: "n1", DepartmentID: &sales},
		{ID: "n2", DepartmentID: &support},
		{ID: "n3"},
	}
	restricted := ScopeOf(uw.DepartmentScope{Restrict: true, DepartmentIDs: []string{"sales"}}, instances)
	if !restricted.Allows("n1") || restricted.Allows("n2") || restricted.Allows("n3") {
		t.Fatalf("restricted scope = %+v", restricted)
	}
	open := ScopeOf(uw.Unrestricted(), instances)
	if open.Restrict || !open.Allows("n2") {
		t.Fatalf("unrestricted scope = %+v", open)
	}
}

func TestRestrictedScopeWithoutNumbersAllowsNothing(t *testing.T) {
	s := ScopeOf(uw.DepartmentScope{Restrict: true}, nil)
	if !s.Restrict || !s.Empty() || s.Allows("n1") {
		t.Fatalf("empty restricted scope = %+v", s)
	}
}

func TestNextRunIsTheNextScheduleThreshold(t *testing.T) {
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	a := Analysis{Trigger: TriggerScheduled, ScheduleHour: 18}
	before := time.Date(2026, 10, 17, 9, 0, 0, 0, loc)
	after := time.Date(2026, 10, 17, 19, 0, 0, 0, loc)

	if got := a.NextRun(before, loc); !got.Equal(time.Date(2026, 10, 17, 18, 0, 0, 0, loc)) {
		t.Fatalf("NextRun before = %v, want today 18:00", got.In(loc))
	}
	if got := a.NextRun(after, loc); !got.Equal(time.Date(2026, 10, 18, 18, 0, 0, 0, loc)) {
		t.Fatalf("NextRun after = %v, want tomorrow 18:00", got.In(loc))
	}
}

func TestRunTallyIsTheMessageSentimentOfOneRun(t *testing.T) {
	r := Run{Status: RunCompleted, MessagesAnalyzed: 9, Result: &Result{
		Sentiment:    SentimentMixed,
		Participants: []ParticipantReading{{Name: "a", Sentiment: SentimentPositive, Messages: 4}, {Name: "b", Sentiment: SentimentNegative, Messages: 5}},
	}}
	got := r.Tally()
	if got.Positive != 4 || got.Negative != 5 || got.Neutral != 0 {
		t.Fatalf("Tally = %+v", got)
	}
	if (Run{Status: RunFailed}).Tally().Total() != 0 {
		t.Fatalf("a failed run has a tally")
	}
}
