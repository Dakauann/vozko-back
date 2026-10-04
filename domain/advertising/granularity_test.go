package advertising

import (
	"errors"
	"testing"
	"time"
)

func day(raw string) time.Time {
	d, _ := ParseDay(raw)
	return d
}

func TestDaysGroupIntoWeeksStartingMondayAndCalendarMonths(t *testing.T) {
	cases := []struct {
		g        Granularity
		in, want string
	}{
		{GranularityDay, "2026-10-04", "2026-10-04"},
		{GranularityWeek, "2026-10-04", "2026-09-28"},
		{GranularityWeek, "2026-09-28", "2026-09-28"},
		{GranularityMonth, "2026-10-04", "2026-10-01"},
	}
	for _, c := range cases {
		if got := c.g.BucketOf(day(c.in)); !got.Equal(day(c.want)) {
			t.Fatalf("%s %s: %s", c.g, c.in, got.Format(DayLayout))
		}
	}
}

func TestOnlyKnownGranularitiesAreAccepted(t *testing.T) {
	if g, err := GranularityOf(""); err != nil || g != GranularityDay {
		t.Fatalf("%q %v", g, err)
	}
	var invalid *ValidationError
	if _, err := GranularityOf("hour"); !errors.As(err, &invalid) {
		t.Fatalf("hour: %v", err)
	}
}
