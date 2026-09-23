package working_hours

import (
	"testing"
	"time"
)

func businessSpec(tz string) *Spec {
	return &Spec{
		Timezone: tz,
		Days: map[string][]Window{
			"mon": {{Start: "09:00", End: "18:00"}},
			"tue": {{Start: "09:00", End: "18:00"}},
			"wed": {{Start: "09:00", End: "18:00"}},
			"thu": {{Start: "09:00", End: "18:00"}},
			"fri": {{Start: "09:00", End: "18:00"}},
		},
	}
}

func compileOrFail(t *testing.T, spec *Spec) *Schedule {
	t.Helper()
	sched, err := spec.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	return sched
}

func localTime(t *testing.T, tz, value string) time.Time {
	t.Helper()
	loc, err := time.LoadLocation(tz)
	if err != nil {
		t.Fatalf("LoadLocation(%q) error = %v", tz, err)
	}
	parsed, err := time.ParseInLocation("2006-01-02 15:04", value, loc)
	if err != nil {
		t.Fatalf("ParseInLocation(%q) error = %v", value, err)
	}
	return parsed
}

func TestOpenDaysCountsBusinessDays(t *testing.T) {
	const tz = "America/Sao_Paulo"
	sched := compileOrFail(t, businessSpec(tz))

	from := localTime(t, tz, "2026-09-01 00:00")
	to := localTime(t, tz, "2026-10-01 00:00")

	got, ok := sched.OpenDays(from, to)
	if !ok {
		t.Fatalf("OpenDays() ok = false, want true")
	}
	if got != 22 {
		t.Fatalf("OpenDays() = %d, want 22", got)
	}
}

func TestOpenDaysSkipsHolidays(t *testing.T) {
	const tz = "America/Sao_Paulo"
	spec := businessSpec(tz)
	spec.Holidays = []string{"2026-09-07", "2026-09-21"}
	sched := compileOrFail(t, spec)

	from := localTime(t, tz, "2026-09-01 00:00")
	to := localTime(t, tz, "2026-10-01 00:00")

	got, ok := sched.OpenDays(from, to)
	if !ok {
		t.Fatalf("OpenDays() ok = false, want true")
	}
	if got != 20 {
		t.Fatalf("OpenDays() = %d, want 20", got)
	}
}

func TestIsOpenFalseOnHoliday(t *testing.T) {
	const tz = "America/Sao_Paulo"
	spec := businessSpec(tz)
	spec.Holidays = []string{"2026-09-07"}
	sched := compileOrFail(t, spec)

	if sched.IsOpen(localTime(t, tz, "2026-09-07 10:00")) {
		t.Fatalf("IsOpen() on a holiday = true, want false")
	}
	if !sched.IsOpen(localTime(t, tz, "2026-09-08 10:00")) {
		t.Fatalf("IsOpen() on the next working day = false, want true")
	}
}

func TestHolidayRemovesItsOpenMinutes(t *testing.T) {
	const tz = "America/Sao_Paulo"
	from := localTime(t, tz, "2026-09-07 00:00")
	to := localTime(t, tz, "2026-09-08 00:00")

	plain := compileOrFail(t, businessSpec(tz))
	minutes, ok := plain.OpenMinutes(from, to)
	if !ok || minutes != 9*60 {
		t.Fatalf("OpenMinutes() = %d, %v, want 540, true", minutes, ok)
	}

	spec := businessSpec(tz)
	spec.Holidays = []string{"2026-09-07"}
	withHoliday := compileOrFail(t, spec)
	minutes, ok = withHoliday.OpenMinutes(from, to)
	if !ok || minutes != 0 {
		t.Fatalf("OpenMinutes() on a holiday = %d, %v, want 0, true", minutes, ok)
	}
}

func TestOpenMinutesAcrossSpringForward(t *testing.T) {
	const tz = "America/New_York"
	sched := compileOrFail(t, &Spec{
		Timezone: tz,
		Days:     map[string][]Window{"sun": {{Start: "00:00", End: "06:00"}}},
	})

	from := localTime(t, tz, "2026-03-08 00:00")
	to := localTime(t, tz, "2026-03-09 00:00")

	got, ok := sched.OpenMinutes(from, to)
	if !ok {
		t.Fatalf("OpenMinutes() ok = false, want true")
	}
	if got != 5*60 {
		t.Fatalf("OpenMinutes() across spring forward = %d, want 300", got)
	}
}

func TestOpenMinutesAcrossFallBack(t *testing.T) {
	const tz = "America/New_York"
	sched := compileOrFail(t, &Spec{
		Timezone: tz,
		Days:     map[string][]Window{"sun": {{Start: "00:00", End: "06:00"}}},
	})

	from := localTime(t, tz, "2026-11-01 00:00")
	to := localTime(t, tz, "2026-11-02 00:00")

	got, ok := sched.OpenMinutes(from, to)
	if !ok {
		t.Fatalf("OpenMinutes() ok = false, want true")
	}
	if got != 7*60 {
		t.Fatalf("OpenMinutes() across fall back = %d, want 420", got)
	}
}

func TestProgressNilScheduleIsNotAvailable(t *testing.T) {
	var sched *Schedule
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)

	if _, _, ok := sched.Progress(from, to, from.AddDate(0, 0, 10)); ok {
		t.Fatalf("Progress() on a nil schedule ok = true, want false")
	}
	if _, ok := sched.OpenDays(from, to); ok {
		t.Fatalf("OpenDays() on a nil schedule ok = true, want false")
	}
	if _, ok := sched.OpenMinutes(from, to); ok {
		t.Fatalf("OpenMinutes() on a nil schedule ok = true, want false")
	}
}

func TestProgressScheduleThatNeverOpens(t *testing.T) {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	sched := New(loc, nil)

	from := time.Date(2026, 9, 1, 0, 0, 0, 0, loc)
	to := from.AddDate(0, 1, 0)

	if _, _, ok := sched.Progress(from, to, from.AddDate(0, 0, 10)); ok {
		t.Fatalf("Progress() on a schedule that never opens ok = true, want false")
	}
}

func TestProgressClampsOutsideThePeriod(t *testing.T) {
	const tz = "America/Sao_Paulo"
	sched := compileOrFail(t, businessSpec(tz))

	from := localTime(t, tz, "2026-09-01 00:00")
	to := localTime(t, tz, "2026-10-01 00:00")

	elapsed, total, ok := sched.Progress(from, to, from.AddDate(0, 0, -5))
	if !ok {
		t.Fatalf("Progress() before the period ok = false, want true")
	}
	if elapsed != 0 {
		t.Fatalf("Progress() before the period elapsed = %d, want 0", elapsed)
	}

	elapsed, totalAfter, ok := sched.Progress(from, to, to.AddDate(0, 0, 5))
	if !ok {
		t.Fatalf("Progress() after the period ok = false, want true")
	}
	if elapsed != total || totalAfter != total {
		t.Fatalf("Progress() after the period = %d/%d, want %d/%d", elapsed, totalAfter, total, total)
	}
}

func TestProgressMidPeriod(t *testing.T) {
	const tz = "America/Sao_Paulo"
	sched := compileOrFail(t, businessSpec(tz))

	from := localTime(t, tz, "2026-09-01 00:00")
	to := localTime(t, tz, "2026-10-01 00:00")
	at := localTime(t, tz, "2026-09-15 13:30")

	elapsed, total, ok := sched.Progress(from, to, at)
	if !ok {
		t.Fatalf("Progress() ok = false, want true")
	}
	if total != 22*9*60 {
		t.Fatalf("Progress() total = %d, want %d", total, 22*9*60)
	}
	want := 10*9*60 + 4*60 + 30
	if elapsed != want {
		t.Fatalf("Progress() elapsed = %d, want %d", elapsed, want)
	}
}

func TestOpenDaysRejectsAnOverwideRange(t *testing.T) {
	const tz = "America/Sao_Paulo"
	sched := compileOrFail(t, businessSpec(tz))

	from := localTime(t, tz, "1990-01-01 00:00")
	to := localTime(t, tz, "2026-01-01 00:00")

	if _, ok := sched.OpenDays(from, to); ok {
		t.Fatalf("OpenDays() over 36 years ok = true, want false")
	}
	if _, ok := sched.OpenMinutes(from, to); ok {
		t.Fatalf("OpenMinutes() over 36 years ok = true, want false")
	}
}

func TestCompileRejectsABadHoliday(t *testing.T) {
	spec := businessSpec("America/Sao_Paulo")
	spec.Holidays = []string{"07/09/2026"}

	if err := spec.Validate(); err == nil {
		t.Fatalf("Validate() with a malformed holiday = nil, want an error")
	} else if !IsPolicyError(err) {
		t.Fatalf("IsPolicyError(%v) = false, want true", err)
	}
}

func TestNormalizedSortsAndDedupesHolidays(t *testing.T) {
	spec := businessSpec("America/Sao_Paulo")
	spec.Holidays = []string{"2026-12-25", " 2026-09-07 ", "2026-12-25", ""}

	got := spec.Normalized().Holidays
	if len(got) != 2 || got[0] != "2026-09-07" || got[1] != "2026-12-25" {
		t.Fatalf("Normalized().Holidays = %v, want [2026-09-07 2026-12-25]", got)
	}
}
