package attendance

import (
	"testing"
	"time"
)

var saoPaulo = func() *time.Location {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		panic(err)
	}
	return loc
}()

func at(day, hour, minute int) time.Time {
	return time.Date(2026, 9, day, hour, minute, 0, 0, saoPaulo)
}

func online(from, to time.Time) PresenceSpan {
	return PresenceSpan{Start: from, End: to}
}

func activityFor(spans []PresenceSpan, handouts []Handout) MemberActivity {
	return BuildMemberActivity(ActivityInput{
		Spans: spans, Handouts: handouts,
		From: at(21, 0, 0), To: at(26, 23, 59), Now: at(27, 12, 0), Location: saoPaulo,
	})
}

func day(a MemberActivity, date string) ActivityDay {
	for _, d := range a.Days {
		if d.Date == date {
			return d
		}
	}
	return ActivityDay{}
}

func hasFlag(d ActivityDay, flag string) bool {
	for _, f := range d.Flags {
		if f == flag {
			return true
		}
	}
	return false
}

func TestDaysFollowTheLocalCalendarAndKeepSessionsWithTheDayTheyStarted(t *testing.T) {
	a := activityFor([]PresenceSpan{online(at(21, 8, 7), at(22, 1, 23))}, nil)
	if len(a.Days) != 6 {
		t.Fatalf("days %d", len(a.Days))
	}
	monday := day(a, "2026-09-21")
	if len(monday.Sessions) != 1 || monday.ConnectedMS != (17*time.Hour+16*time.Minute).Milliseconds() {
		t.Fatalf("monday %+v", monday)
	}
	if len(day(a, "2026-09-22").Sessions) != 0 {
		t.Fatal("the session belongs to the day it started")
	}
}

func TestAdjacentIntervalsBecomeOneSessionWithItsCallTime(t *testing.T) {
	a := activityFor([]PresenceSpan{
		online(at(23, 9, 0), at(23, 11, 0)),
		{Start: at(23, 11, 0), End: at(23, 11, 30), OnCall: true},
		online(at(23, 11, 30), at(23, 18, 0)),
	}, nil)
	d := day(a, "2026-09-23")
	if len(d.Sessions) != 1 || d.OnCallMS != (30*time.Minute).Milliseconds() || d.ConnectedMS != (9*time.Hour).Milliseconds() {
		t.Fatalf("day %+v", d)
	}
}

func TestAVeryLongSessionIsFlaggedAsAPossibleForgottenTab(t *testing.T) {
	a := activityFor([]PresenceSpan{online(at(24, 8, 19), at(25, 8, 9)), online(at(25, 8, 9), at(25, 18, 25))}, nil)
	if !hasFlag(day(a, "2026-09-24"), FlagPossibleForgottenTab) {
		t.Fatal("a 24h session must be flagged")
	}
}

func TestALateStartIsMeasuredAgainstThePersonsUsualStart(t *testing.T) {
	spans := []PresenceSpan{
		online(at(21, 8, 0), at(21, 18, 0)),
		online(at(22, 8, 10), at(22, 18, 0)),
		online(at(23, 8, 5), at(23, 18, 0)),
		online(at(24, 15, 57), at(24, 18, 0)),
	}
	a := activityFor(spans, nil)
	if a.UsualStart != "08:05" {
		t.Fatalf("usual start %q", a.UsualStart)
	}
	if !hasFlag(day(a, "2026-09-24"), FlagLateStart) || hasFlag(day(a, "2026-09-22"), FlagLateStart) {
		t.Fatal("only the afternoon start is late")
	}
}

func TestAWeekdayWithoutPresenceIsFlaggedOnlyWhenThePersonUsuallyWorksIt(t *testing.T) {
	in := ActivityInput{
		Spans: []PresenceSpan{
			online(at(1, 9, 0), at(1, 18, 0)), online(at(8, 9, 0), at(8, 18, 0)),
			online(at(2, 9, 0), at(2, 18, 0)),
		},
		From: at(1, 0, 0), To: at(16, 23, 59), Now: at(17, 12, 0), Location: saoPaulo,
	}
	a := BuildMemberActivity(in)
	if !hasFlag(day(a, "2026-09-15"), FlagNoPresence) {
		t.Fatal("a missed usual Tuesday is flagged")
	}
	if hasFlag(day(a, "2026-09-13"), FlagNoPresence) {
		t.Fatal("a Sunday the person never works is not flagged")
	}
}

func TestHandoutsAreCountedAndOnesWhileOfflineStandOut(t *testing.T) {
	a := activityFor(
		[]PresenceSpan{online(at(22, 9, 0), at(22, 18, 0))},
		[]Handout{{At: at(22, 10, 0), Trigger: "inbound_rr"}, {At: at(22, 20, 0), Trigger: "inbound_rr"}, {At: at(22, 21, 0), Trigger: "manual"}},
	)
	if a.Received["inbound_rr"] != 2 || a.Received["manual"] != 1 || a.ReceivedWhileOffline != 1 {
		t.Fatalf("received %+v offline %d", a.Received, a.ReceivedWhileOffline)
	}
}

func TestTheHeatmapSpreadsConnectedTimeOverLocalHours(t *testing.T) {
	a := activityFor([]PresenceSpan{online(at(23, 9, 30), at(23, 11, 0))}, nil)
	wednesday := int(time.Wednesday)
	if a.Heatmap[wednesday][9] != 30 || a.Heatmap[wednesday][10] != 60 || a.Heatmap[wednesday][11] != 0 {
		t.Fatalf("heatmap %v", a.Heatmap[wednesday])
	}
}

func TestAnOpenSessionRunsUntilNow(t *testing.T) {
	in := ActivityInput{
		Spans: []PresenceSpan{{Start: at(27, 9, 0), Open: true}},
		From:  at(27, 0, 0), To: at(27, 23, 59), Now: at(27, 12, 0), Location: saoPaulo,
	}
	d := BuildMemberActivity(in).Days[0]
	if len(d.Sessions) != 1 || !d.Sessions[0].Open || d.ConnectedMS != (3*time.Hour).Milliseconds() {
		t.Fatalf("day %+v", d)
	}
}
