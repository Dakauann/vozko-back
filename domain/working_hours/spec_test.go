package working_hours

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mondayNineToSix() *Spec {
	return &Spec{
		Timezone: "America/Sao_Paulo",
		Days:     map[string][]Window{"mon": {{Start: "09:00", End: "18:00"}}},
	}
}

// The embedded tzdata is what makes an arbitrary IANA zone work on a host with
// no zoneinfo. If this fails, every schedule silently degrades to always-open.
func TestSpec_LoadsAnIANAZoneWithoutHostTzdata(t *testing.T) {
	for _, zone := range []string{"America/Sao_Paulo", "UTC", "Europe/Lisbon", "America/New_York"} {
		_, err := (&Spec{Timezone: zone, Days: map[string][]Window{"mon": {{Start: "09:00", End: "18:00"}}}}).Compile()
		assert.NoError(t, err, zone)
	}
}

func TestSpec_CompilesToAWorkingSchedule(t *testing.T) {
	sched, err := mondayNineToSix().Compile()
	require.NoError(t, err)

	loc := sched.Location()
	assert.True(t, sched.IsOpen(time.Date(2026, 9, 7, 12, 0, 0, 0, loc)), "monday noon")
	assert.False(t, sched.IsOpen(time.Date(2026, 9, 8, 12, 0, 0, 0, loc)), "tuesday is not configured")
}

func TestSpec_NilIsAlwaysOpen(t *testing.T) {
	var s *Spec
	sched, err := s.Compile()
	require.NoError(t, err)
	assert.Nil(t, sched)
	assert.True(t, sched.IsOpen(time.Now()), "a nil schedule is always open")
}

// End at or before start runs past midnight. One row in the UI, one interval in
// the domain, instead of two halves on two weekdays.
func TestSpec_EndBeforeStartIsAnOvernightShift(t *testing.T) {
	s := &Spec{
		Timezone: "America/Sao_Paulo",
		Days:     map[string][]Window{"mon": {{Start: "22:00", End: "02:00"}}},
	}
	sched, err := s.Compile()
	require.NoError(t, err)
	loc := sched.Location()

	assert.True(t, sched.IsOpen(time.Date(2026, 9, 7, 23, 0, 0, 0, loc)), "monday night")
	assert.True(t, sched.IsOpen(time.Date(2026, 9, 8, 1, 0, 0, 0, loc)), "spills into tuesday")
	assert.False(t, sched.IsOpen(time.Date(2026, 9, 8, 3, 0, 0, 0, loc)), "after it ends")
}

func TestSpec_TwentyFourHundredIsEndOfDay(t *testing.T) {
	s := &Spec{
		Timezone: "UTC",
		Days:     map[string][]Window{"mon": {{Start: "00:00", End: "24:00"}}},
	}
	sched, err := s.Compile()
	require.NoError(t, err)

	assert.True(t, sched.IsOpen(time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)), "from midnight")
	assert.True(t, sched.IsOpen(time.Date(2026, 9, 7, 23, 59, 0, 0, time.UTC)), "to the last minute")
	assert.Equal(t, 24*time.Hour, sched.Elapsed(
		time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)), "a whole open day")
}

func TestSpec_LunchBreakIsTwoWindowsOnOneDay(t *testing.T) {
	s := &Spec{
		Timezone: "UTC",
		Days: map[string][]Window{"mon": {
			{Start: "09:00", End: "12:00"},
			{Start: "13:00", End: "18:00"},
		}},
	}
	sched, err := s.Compile()
	require.NoError(t, err)
	assert.Equal(t, 2*time.Hour, sched.Elapsed(
		time.Date(2026, 9, 7, 11, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 7, 14, 0, 0, 0, time.UTC)))
}

// -- rejections ---------------------------------------------------------------

func TestSpec_RejectsAnUnknownTimezone(t *testing.T) {
	s := &Spec{Timezone: "Mars/Olympus_Mons", Days: map[string][]Window{"mon": {{Start: "09:00", End: "18:00"}}}}
	assert.ErrorIs(t, s.Validate(), ErrUnknownTimezone)
}

func TestSpec_RejectsAnEmptyTimezone(t *testing.T) {
	s := &Spec{Days: map[string][]Window{"mon": {{Start: "09:00", End: "18:00"}}}}
	assert.ErrorIs(t, s.Validate(), ErrUnknownTimezone)
}

func TestSpec_RejectsAnUnknownWeekday(t *testing.T) {
	s := &Spec{Timezone: "UTC", Days: map[string][]Window{"funday": {{Start: "09:00", End: "18:00"}}}}
	assert.ErrorIs(t, s.Validate(), ErrUnknownWeekday)
}

func TestSpec_RejectsMalformedTimes(t *testing.T) {
	for _, bad := range []string{"9am", "", "09", "09:60", "25:00", "09:00:00", "ab:cd"} {
		s := &Spec{Timezone: "UTC", Days: map[string][]Window{"mon": {{Start: bad, End: "18:00"}}}}
		assert.ErrorIs(t, s.Validate(), ErrBadTime, "start %q", bad)
	}
}

// Ambiguous between "closed" and "open all day", so it is refused and the admin
// is pushed to 00:00-24:00.
func TestSpec_RejectsEqualStartAndEnd(t *testing.T) {
	s := &Spec{Timezone: "UTC", Days: map[string][]Window{"mon": {{Start: "09:00", End: "09:00"}}}}
	assert.ErrorIs(t, s.Validate(), ErrIntervalEmpty)
}

func TestSpec_RejectsTwentyFourHundredAsAStart(t *testing.T) {
	s := &Spec{Timezone: "UTC", Days: map[string][]Window{"mon": {{Start: "24:00", End: "02:00"}}}}
	assert.ErrorIs(t, s.Validate(), ErrIntervalOutOfRange)
}

// A schedule with no window anywhere would freeze every deadline in its scope,
// and in a UI showing an empty week it is indistinguishable from "not set".
func TestSpec_RejectsAWeekWithNoOpenTime(t *testing.T) {
	assert.ErrorIs(t, (&Spec{Timezone: "UTC"}).Validate(), ErrNoOpenTime)
	assert.ErrorIs(t, (&Spec{Timezone: "UTC", Days: map[string][]Window{"mon": {}}}).Validate(), ErrNoOpenTime)
}

func TestSpec_RejectsOverlappingWindowsOnOneDay(t *testing.T) {
	s := &Spec{
		Timezone: "UTC",
		Days: map[string][]Window{"mon": {
			{Start: "09:00", End: "13:00"},
			{Start: "12:00", End: "18:00"},
		}},
	}
	assert.ErrorIs(t, s.Validate(), ErrIntervalOverlap)
}

func TestSpec_NilValidatesAsNotConfigured(t *testing.T) {
	var s *Spec
	assert.NoError(t, s.Validate())
}

// -- normalization and round-trip ---------------------------------------------

func TestSpec_NormalizedPadsTimesAndOrdersDays(t *testing.T) {
	s := &Spec{
		Timezone: " America/Sao_Paulo ",
		Days: map[string][]Window{
			"fri": {{Start: "9:00", End: "18:00"}},
			"mon": {{Start: "13:00", End: "18:00"}, {Start: "9:00", End: "12:00"}},
			"sat": {},
		},
	}
	got := s.Normalized()

	assert.Equal(t, "America/Sao_Paulo", got.Timezone, "trimmed")
	assert.Equal(t, []Window{{Start: "09:00", End: "12:00"}, {Start: "13:00", End: "18:00"}}, got.Days["mon"],
		"zero-padded and ordered by start")
	assert.NotContains(t, got.Days, "sat", "an empty day is dropped rather than stored as noise")

	// Key order is canonical, so repeated writes produce the same document.
	raw, err := json.Marshal(got)
	require.NoError(t, err)
	assert.JSONEq(t,
		`{"timezone":"America/Sao_Paulo","days":{"mon":[{"start":"09:00","end":"12:00"},{"start":"13:00","end":"18:00"}],"fri":[{"start":"09:00","end":"18:00"}]}}`,
		string(raw))
}

func TestSpec_SurvivesAJSONRoundTrip(t *testing.T) {
	original := mondayNineToSix()
	raw, err := json.Marshal(original)
	require.NoError(t, err)

	var back Spec
	require.NoError(t, json.Unmarshal(raw, &back))
	require.NoError(t, back.Validate())

	sched, err := back.Compile()
	require.NoError(t, err)
	assert.True(t, sched.IsOpen(time.Date(2026, 9, 7, 12, 0, 0, 0, sched.Location())))
}

// -- the property the sweep depends on ----------------------------------------

// A department's hours replace the workspace's; without its own it inherits.
func TestSpec_ResolvesDepartmentOverWorkspace(t *testing.T) {
	ws, err := mondayNineToSix().Compile()
	require.NoError(t, err)
	dept, err := (&Spec{
		Timezone: "America/Sao_Paulo",
		Days:     map[string][]Window{"sat": {{Start: "08:00", End: "20:00"}}},
	}).Compile()
	require.NoError(t, err)

	loc := ws.Location()
	assert.True(t, Resolve(ws, dept).IsOpen(time.Date(2026, 9, 12, 10, 0, 0, 0, loc)), "saturday, department hours")
	assert.False(t, Resolve(ws, dept).IsOpen(time.Date(2026, 9, 7, 12, 0, 0, 0, loc)), "monday is not inherited back")
	assert.True(t, Resolve(ws, nil).IsOpen(time.Date(2026, 9, 7, 12, 0, 0, 0, loc)), "no department hours inherits")
}
