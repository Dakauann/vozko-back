package working_hours

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A fixed zone rather than a loaded one: the build host may have no tz
// database, and Brazil has observed no DST since 2019. Same reasoning as
// domain/billing/location.go.
var brt = time.FixedZone("BRT", -3*60*60)

func hm(h, m int) int { return h*60 + m }

func at(y int, mo time.Month, d, h, m int) time.Time {
	return time.Date(y, mo, d, h, m, 0, 0, brt)
}

// Calendar anchors these tests depend on. If this fails, every other date
// assertion below is meaningless, so it is asserted rather than assumed.
func TestFixtureCalendarIsWhatTheTestsAssume(t *testing.T) {
	assert.Equal(t, time.Monday, at(2026, 9, 7, 0, 0).Weekday())
	assert.Equal(t, time.Friday, at(2026, 9, 11, 0, 0).Weekday())
	assert.Equal(t, time.Saturday, at(2026, 9, 12, 0, 0).Weekday())
	assert.Equal(t, time.Sunday, at(2026, 9, 13, 0, 0).Weekday())
	assert.Equal(t, time.Monday, at(2026, 9, 14, 0, 0).Weekday())
}

// office is Mon-Fri 09:00-18:00.
func office() *Schedule {
	iv := []Interval{{StartMin: hm(9, 0), EndMin: hm(18, 0)}}
	return New(brt, map[time.Weekday][]Interval{
		time.Monday: iv, time.Tuesday: iv, time.Wednesday: iv,
		time.Thursday: iv, time.Friday: iv,
	})
}

// -- IsOpen ------------------------------------------------------------------

func TestIsOpen_InsideTheWindow(t *testing.T) {
	assert.True(t, office().IsOpen(at(2026, 9, 7, 12, 0)))
}

// Half-open [start, end): the minute the office opens is open, the minute it
// closes is not. Anything else makes "09:00-18:00" and "18:00-22:00" overlap.
func TestIsOpen_BoundariesAreHalfOpen(t *testing.T) {
	s := office()
	assert.True(t, s.IsOpen(at(2026, 9, 7, 9, 0)), "opening minute is open")
	assert.False(t, s.IsOpen(at(2026, 9, 7, 8, 59)), "a minute before is closed")
	assert.False(t, s.IsOpen(at(2026, 9, 7, 18, 0)), "closing minute is closed")
	assert.True(t, s.IsOpen(at(2026, 9, 7, 17, 59)), "a minute before close is open")
}

func TestIsOpen_DayWithNoIntervalsIsClosed(t *testing.T) {
	assert.False(t, office().IsOpen(at(2026, 9, 12, 12, 0)), "saturday")
	assert.False(t, office().IsOpen(at(2026, 9, 13, 12, 0)), "sunday")
}

// A support desk running 22:00-02:00 is a single interval whose end runs past
// midnight, not two intervals on two days.
func TestIsOpen_OvernightShift(t *testing.T) {
	s := New(brt, map[time.Weekday][]Interval{
		time.Monday: {{StartMin: hm(22, 0), EndMin: hm(26, 0)}},
	})
	assert.True(t, s.IsOpen(at(2026, 9, 7, 23, 0)), "monday night")
	assert.True(t, s.IsOpen(at(2026, 9, 8, 1, 0)), "spills into tuesday")
	assert.False(t, s.IsOpen(at(2026, 9, 8, 2, 0)), "ends at 02:00")
	assert.False(t, s.IsOpen(at(2026, 9, 7, 21, 59)), "before it starts")
}

// The backward-compatible default. Every workspace that never configures hours
// must keep behaving exactly as it does today.
func TestIsOpen_NilScheduleIsAlwaysOpen(t *testing.T) {
	var s *Schedule
	assert.True(t, s.IsOpen(at(2026, 9, 13, 3, 0)), "sunday at 3am")
}

// -- Elapsed: the theory -----------------------------------------------------

func TestElapsed_WithinASingleDay(t *testing.T) {
	got := office().Elapsed(at(2026, 9, 7, 10, 0), at(2026, 9, 7, 12, 30))
	assert.Equal(t, 150*time.Minute, got)
}

func TestElapsed_ClipsToTheOpenWindow(t *testing.T) {
	// 06:00 -> 20:00 spans the whole working day and nothing else counts.
	got := office().Elapsed(at(2026, 9, 7, 6, 0), at(2026, 9, 7, 20, 0))
	assert.Equal(t, 9*time.Hour, got)
}

// THE scenario the feature exists for.
func TestElapsed_TheRescueScenario(t *testing.T) {
	s := office()
	handout := at(2026, 9, 7, 17, 55) // Monday, five minutes before close

	assert.Equal(t, 5*time.Minute, s.Elapsed(handout, at(2026, 9, 7, 18, 0)),
		"five minutes accrue before close")
	assert.Equal(t, 5*time.Minute, s.Elapsed(handout, at(2026, 9, 8, 3, 0)),
		"nothing accrues overnight")
	assert.Equal(t, 5*time.Minute, s.Elapsed(handout, at(2026, 9, 8, 9, 0)),
		"still five at the moment the office reopens")
	assert.Equal(t, 14*time.Minute, s.Elapsed(handout, at(2026, 9, 8, 9, 9)),
		"one minute short of a 15m budget")
	assert.Equal(t, 15*time.Minute, s.Elapsed(handout, at(2026, 9, 8, 9, 10)),
		"due at 09:10, not at 18:10 the night before")
}

func TestElapsed_SkipsTheWeekend(t *testing.T) {
	s := office()
	handout := at(2026, 9, 11, 17, 55) // Friday
	assert.Equal(t, 5*time.Minute, s.Elapsed(handout, at(2026, 9, 13, 23, 0)),
		"saturday and sunday add nothing")
	assert.Equal(t, 15*time.Minute, s.Elapsed(handout, at(2026, 9, 14, 9, 10)),
		"due monday morning")
}

func TestElapsed_LunchBreakDoesNotCount(t *testing.T) {
	s := New(brt, map[time.Weekday][]Interval{
		time.Monday: {
			{StartMin: hm(9, 0), EndMin: hm(12, 0)},
			{StartMin: hm(13, 0), EndMin: hm(18, 0)},
		},
	})
	assert.Equal(t, 2*time.Hour, s.Elapsed(at(2026, 9, 7, 11, 0), at(2026, 9, 7, 14, 0)),
		"one hour before lunch, one after, the hour of lunch skipped")
}

func TestElapsed_NilScheduleIsWallClock(t *testing.T) {
	var s *Schedule
	from, to := at(2026, 9, 12, 2, 0), at(2026, 9, 13, 2, 0)
	assert.Equal(t, 24*time.Hour, s.Elapsed(from, to))
}

func TestElapsed_ReversedOrEmptyRangeIsZero(t *testing.T) {
	s := office()
	assert.Zero(t, s.Elapsed(at(2026, 9, 7, 12, 0), at(2026, 9, 7, 10, 0)), "reversed")
	assert.Zero(t, s.Elapsed(at(2026, 9, 7, 12, 0), at(2026, 9, 7, 12, 0)), "empty")
}

// A schedule that never opens accrues nothing, forever. That is faithful to
// what was configured, and it is why Validate rejects it.
func TestElapsed_NeverOpenScheduleAccruesNothing(t *testing.T) {
	s := New(brt, nil)
	assert.Zero(t, s.Elapsed(at(2026, 9, 7, 0, 0), at(2026, 10, 7, 0, 0)))
}

func TestElapsed_OvernightShiftAccruesAcrossMidnight(t *testing.T) {
	s := New(brt, map[time.Weekday][]Interval{
		time.Monday: {{StartMin: hm(22, 0), EndMin: hm(26, 0)}},
	})
	assert.Equal(t, 3*time.Hour, s.Elapsed(at(2026, 9, 7, 23, 0), at(2026, 9, 8, 5, 0)))
}

// An overnight shift that runs into the next day's own window describes the
// same wall-clock minutes twice. They must be counted once.
func TestElapsed_DoesNotDoubleCountOverlappingSegments(t *testing.T) {
	s := New(brt, map[time.Weekday][]Interval{
		time.Monday:  {{StartMin: hm(22, 0), EndMin: hm(30, 0)}}, // mon 22:00 -> tue 06:00
		time.Tuesday: {{StartMin: hm(0, 0), EndMin: hm(8, 0)}},   // tue 00:00 -> 08:00
	})
	// Real span is mon 22:00 -> tue 08:00 = 10h, not 8h + 8h.
	assert.Equal(t, 10*time.Hour, s.Elapsed(at(2026, 9, 7, 22, 0), at(2026, 9, 8, 8, 0)))
}

func TestElapsed_MultiWeekSpan(t *testing.T) {
	// Mon 09:00 through the following Mon 09:00: five 9h working days.
	got := office().Elapsed(at(2026, 9, 7, 9, 0), at(2026, 9, 14, 9, 0))
	assert.Equal(t, 45*time.Hour, got, "five 9h days")
}

// -- NextOpen ----------------------------------------------------------------

func TestNextOpen_WhenAlreadyOpenReturnsNow(t *testing.T) {
	now := at(2026, 9, 7, 12, 0)
	got, ok := office().NextOpen(now)
	require.True(t, ok)
	assert.Equal(t, now, got)
}

func TestNextOpen_JumpsToTheNextMorning(t *testing.T) {
	got, ok := office().NextOpen(at(2026, 9, 7, 19, 0))
	require.True(t, ok)
	assert.True(t, at(2026, 9, 8, 9, 0).Equal(got), "expected tue 09:00, got %s", got)
}

func TestNextOpen_JumpsAcrossTheWeekend(t *testing.T) {
	got, ok := office().NextOpen(at(2026, 9, 12, 10, 0)) // saturday
	require.True(t, ok)
	assert.True(t, at(2026, 9, 14, 9, 0).Equal(got), "expected mon 09:00, got %s", got)
}

func TestNextOpen_NeverOpenScheduleReportsNoNextOpening(t *testing.T) {
	_, ok := New(brt, nil).NextOpen(at(2026, 9, 7, 12, 0))
	assert.False(t, ok)
}

func TestNextOpen_NilScheduleIsOpenNow(t *testing.T) {
	var s *Schedule
	now := at(2026, 9, 13, 3, 0)
	got, ok := s.NextOpen(now)
	require.True(t, ok)
	assert.Equal(t, now, got)
}

// -- Resolve: workspace vs department ----------------------------------------

func TestResolve_DepartmentOverridesWorkspace(t *testing.T) {
	ws := office()
	dept := New(brt, map[time.Weekday][]Interval{
		time.Saturday: {{StartMin: hm(8, 0), EndMin: hm(20, 0)}},
	})
	got := Resolve(ws, dept)
	assert.True(t, got.IsOpen(at(2026, 9, 12, 10, 0)), "saturday open for this department")
	assert.False(t, got.IsOpen(at(2026, 9, 7, 12, 0)), "monday is not: it overrides, it does not merge")
}

func TestResolve_DepartmentWithoutHoursInheritsWorkspace(t *testing.T) {
	got := Resolve(office(), nil)
	assert.True(t, got.IsOpen(at(2026, 9, 7, 12, 0)))
	assert.False(t, got.IsOpen(at(2026, 9, 12, 10, 0)))
}

func TestResolve_NeitherConfiguredIsAlwaysOpen(t *testing.T) {
	got := Resolve(nil, nil)
	assert.True(t, got.IsOpen(at(2026, 9, 13, 3, 0)))
	assert.Equal(t, time.Hour, got.Elapsed(at(2026, 9, 13, 3, 0), at(2026, 9, 13, 4, 0)))
}

// -- Validate ----------------------------------------------------------------

func TestValidate_AcceptsAnOrdinaryWeek(t *testing.T) {
	assert.NoError(t, office().Validate())
}

func TestValidate_NilIsValid(t *testing.T) {
	var s *Schedule
	assert.NoError(t, s.Validate())
}

// The foot-gun: a schedule with no open time at all would silently disable the
// rescue for that workspace forever.
func TestValidate_RejectsAScheduleThatNeverOpens(t *testing.T) {
	assert.ErrorIs(t, New(brt, nil).Validate(), ErrNoOpenTime)
}

func TestValidate_RejectsEmptyAndReversedIntervals(t *testing.T) {
	s := New(brt, map[time.Weekday][]Interval{
		time.Monday: {{StartMin: hm(18, 0), EndMin: hm(9, 0)}},
	})
	assert.ErrorIs(t, s.Validate(), ErrIntervalEmpty)
}

func TestValidate_RejectsOutOfRangeMinutes(t *testing.T) {
	s := New(brt, map[time.Weekday][]Interval{
		time.Monday: {{StartMin: -1, EndMin: hm(9, 0)}},
	})
	assert.ErrorIs(t, s.Validate(), ErrIntervalOutOfRange)

	s2 := New(brt, map[time.Weekday][]Interval{
		time.Monday: {{StartMin: hm(9, 0), EndMin: 3 * 24 * 60}},
	})
	assert.ErrorIs(t, s2.Validate(), ErrIntervalOutOfRange)
}

func TestValidate_RejectsOverlappingIntervalsOnTheSameDay(t *testing.T) {
	s := New(brt, map[time.Weekday][]Interval{
		time.Monday: {
			{StartMin: hm(9, 0), EndMin: hm(13, 0)},
			{StartMin: hm(12, 0), EndMin: hm(18, 0)},
		},
	})
	assert.ErrorIs(t, s.Validate(), ErrIntervalOverlap)
}

func TestValidate_RequiresATimezone(t *testing.T) {
	s := New(nil, map[time.Weekday][]Interval{
		time.Monday: {{StartMin: hm(9, 0), EndMin: hm(18, 0)}},
	})
	assert.ErrorIs(t, s.Validate(), ErrNoLocation)
}
