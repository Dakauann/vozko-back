package opportunity

import (
	"errors"
	"math"
	"testing"
	"time"
)

var (
	at        = time.Date(2026, 9, 25, 14, 0, 0, 0, time.UTC)
	later     = at.Add(2 * time.Hour)
	openStage = StageRef{ID: "stage1", PipelineID: "pipe1"}
	wonStage  = StageRef{ID: "stage-won", PipelineID: "pipe1", IsWon: true}
	lostStage = StageRef{ID: "stage-lost", PipelineID: "pipe1", IsLost: true}
)

func TestPlaceOnDerivesTheStatusFromTheStage(t *testing.T) {
	cases := []struct {
		name  string
		stage StageRef
		want  Status
	}{
		{name: "open stage", stage: openStage, want: StatusOpen},
		{name: "won stage", stage: wonStage, want: StatusWon},
		{name: "lost stage", stage: lostStage, want: StatusLost},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := baseOpp()
			if err := o.PlaceOn(tc.stage, "u2", at); err != nil {
				t.Fatalf("PlaceOn() error = %v", err)
			}
			if o.Status != tc.want || o.StageID != tc.stage.ID {
				t.Fatalf("PlaceOn() = %s on %s, want %s on %s", o.Status, o.StageID, tc.want, tc.stage.ID)
			}
		})
	}
}

func TestEnteringAClosedStageStampsWhenAndWho(t *testing.T) {
	o := baseOpp()
	if err := o.PlaceOn(wonStage, "ai:agent-1", at); err != nil {
		t.Fatalf("PlaceOn() error = %v", err)
	}
	if o.CloseDate == nil || !o.CloseDate.Equal(at) || o.ClosedBy != "ai:agent-1" {
		t.Fatalf("closed at %v by %q, want %v by ai:agent-1", o.CloseDate, o.ClosedBy, at)
	}
}

func TestMovingBetweenClosedStagesRestampsTheClose(t *testing.T) {
	o := baseOpp()
	o.LostReasonID = "price"
	_ = o.PlaceOn(wonStage, "u1", at)
	if err := o.PlaceOn(lostStage, "u2", later); err != nil {
		t.Fatalf("PlaceOn() error = %v", err)
	}
	if !o.CloseDate.Equal(later) || o.ClosedBy != "u2" {
		t.Fatalf("closed at %v by %q, want %v by u2", o.CloseDate, o.ClosedBy, later)
	}
}

func TestStayingOnAClosedStageKeepsTheOriginalClose(t *testing.T) {
	o := baseOpp()
	_ = o.PlaceOn(wonStage, "u1", at)
	if err := o.PlaceOn(wonStage, "u2", later); err != nil {
		t.Fatalf("PlaceOn() error = %v", err)
	}
	if !o.CloseDate.Equal(at) || o.ClosedBy != "u1" {
		t.Fatalf("closed at %v by %q, want the original %v by u1", o.CloseDate, o.ClosedBy, at)
	}
}

func TestReopeningClearsTheClose(t *testing.T) {
	o := baseOpp()
	_ = o.PlaceOn(wonStage, "u1", at)
	if err := o.PlaceOn(openStage, "u2", later); err != nil {
		t.Fatalf("PlaceOn() error = %v", err)
	}
	if o.Status != StatusOpen || o.CloseDate != nil || o.ClosedBy != "" {
		t.Fatalf("reopened deal = %s closed %v by %q, want open with no close", o.Status, o.CloseDate, o.ClosedBy)
	}
}

func TestPlaceOnRefusesAStageFromAnotherPipeline(t *testing.T) {
	o := baseOpp()
	err := o.PlaceOn(StageRef{ID: "other", PipelineID: "pipe2", IsWon: true}, "u1", at)
	if !errors.Is(err, ErrStageOutsidePipeline) {
		t.Fatalf("PlaceOn() error = %v, want ErrStageOutsidePipeline", err)
	}
	if o.StageID != "stage1" || o.Status != StatusOpen || o.CloseDate != nil {
		t.Fatalf("a refused move changed the deal: %+v", o)
	}
}

func TestValidateRefusesAWonDealWithoutAValue(t *testing.T) {
	o := baseOpp()
	o.ValueCents = 0
	o.Status = StatusWon
	if err := o.Validate(); !errors.Is(err, ErrWonWithoutValue) {
		t.Fatalf("Validate() error = %v, want ErrWonWithoutValue", err)
	}
}

func TestValidateAllowsAnOpenOrLostDealWithoutAValue(t *testing.T) {
	open := baseOpp()
	open.ValueCents = 0
	lost := baseOpp()
	lost.ValueCents = 0
	lost.Status = StatusLost
	lost.LostReasonID = "price"
	for _, o := range []*Opportunity{open, lost} {
		if err := o.Validate(); err != nil {
			t.Fatalf("Validate(%s, no value) error = %v", o.Status, err)
		}
	}
}

func TestValidateOnlyAcceptsSupportedCurrencies(t *testing.T) {
	for _, currency := range SupportedCurrencies() {
		o := baseOpp()
		o.Currency = currency
		if err := o.Validate(); err != nil {
			t.Fatalf("Validate(%s) error = %v", currency, err)
		}
	}
	o := baseOpp()
	o.Currency = "BRLX"
	if err := o.Validate(); !errors.Is(err, ErrUnsupportedCurrency) {
		t.Fatalf("Validate(BRLX) error = %v, want ErrUnsupportedCurrency", err)
	}
}

func TestChangesOfANewDealIsACreation(t *testing.T) {
	o := baseOpp()
	o.ID = "opp1"
	events := Changes(nil, o, "ai:agent-1", at)
	if len(events) != 1 || events[0].Type != EventCreated {
		t.Fatalf("Changes(nil, o) = %+v, want one created event", events)
	}
	e := events[0]
	if e.ActorID != "ai:agent-1" || e.OpportunityID != "opp1" || e.WorkspaceID != "ws1" || !e.CreatedAt.Equal(at) {
		t.Fatalf("created event = %+v", e)
	}
	if e.ToStageID != "stage1" || e.ValueCents != 490000 || e.Currency != "BRL" {
		t.Fatalf("created event does not carry the starting state: %+v", e)
	}
}

func TestChangesNamesEachThingThatMoved(t *testing.T) {
	before := baseOpp()
	before.ID = "opp1"
	after := *before
	_ = after.PlaceOn(wonStage, "u2", at)
	after.ValueCents = 700000
	after.OwnerID = "u3"

	events := Changes(before, &after, "u2", at)
	got := map[EventType]Event{}
	for _, e := range events {
		got[e.Type] = e
	}
	for _, want := range []EventType{EventStageMoved, EventWon, EventValueChanged, EventOwnerChanged} {
		if _, ok := got[want]; !ok {
			t.Fatalf("Changes() = %v, missing %s", events, want)
		}
	}
	if got[EventStageMoved].FromStageID != "stage1" || got[EventStageMoved].ToStageID != "stage-won" {
		t.Fatalf("stage event = %+v", got[EventStageMoved])
	}
	if got[EventValueChanged].Details["from_value_cents"] != int64(490000) || got[EventValueChanged].ValueCents != 700000 {
		t.Fatalf("value event = %+v", got[EventValueChanged])
	}
	if got[EventOwnerChanged].Details["from_owner_id"] != "u1" || got[EventOwnerChanged].Details["to_owner_id"] != "u3" {
		t.Fatalf("owner event = %+v", got[EventOwnerChanged])
	}
}

func TestChangesSaysLostAndReopened(t *testing.T) {
	before := baseOpp()
	lost := *before
	lost.LostReasonID = "price"
	_ = lost.PlaceOn(lostStage, "u1", at)
	if !hasEvent(Changes(before, &lost, "u1", at), EventLost) {
		t.Fatalf("moving to a lost stage produced no lost event")
	}
	reopened := lost
	_ = reopened.PlaceOn(openStage, "u1", later)
	if !hasEvent(Changes(&lost, &reopened, "u1", later), EventReopened) {
		t.Fatalf("leaving a lost stage produced no reopened event")
	}
}

func TestChangesOfNothingIsNothing(t *testing.T) {
	o := baseOpp()
	same := *o
	if events := Changes(o, &same, "u1", at); len(events) != 0 {
		t.Fatalf("Changes() of an unchanged deal = %v", events)
	}
}

func hasEvent(events []Event, want EventType) bool {
	for _, e := range events {
		if e.Type == want {
			return true
		}
	}
	return false
}

func TestCentsFromAmount(t *testing.T) {
	cases := []struct {
		amount float64
		want   int64
	}{
		{amount: 79, want: 7900},
		{amount: 1234.56, want: 123456},
		{amount: 0.1 + 0.2, want: 30},
		{amount: 19.999, want: 2000},
	}
	for _, tc := range cases {
		got, err := CentsFromAmount(tc.amount)
		if err != nil || got != tc.want {
			t.Fatalf("CentsFromAmount(%v) = %d, %v, want %d", tc.amount, got, err, tc.want)
		}
	}
	for _, bad := range []float64{-1, math.NaN(), math.Inf(1), 1e18} {
		if _, err := CentsFromAmount(bad); !errors.Is(err, ErrInvalidAmount) {
			t.Fatalf("CentsFromAmount(%v) error = %v, want ErrInvalidAmount", bad, err)
		}
	}
}

func TestCentsFromText(t *testing.T) {
	for text, want := range map[string]int64{"79": 7900, " 1500.50 ": 150050, "0.1": 10} {
		got, err := CentsFromText(text)
		if err != nil || got != want {
			t.Fatalf("CentsFromText(%q) = %d, %v, want %d", text, got, err, want)
		}
	}
	for _, bad := range []string{"", "mil reais", "1.500,50", "NaN", "-5"} {
		if _, err := CentsFromText(bad); !errors.Is(err, ErrInvalidAmount) {
			t.Fatalf("CentsFromText(%q) error = %v, want ErrInvalidAmount", bad, err)
		}
	}
}
