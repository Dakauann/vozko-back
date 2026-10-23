package emailsending

import (
	"testing"
	"time"
)

func TestTimeZoneNameIsTheProductZone(t *testing.T) {
	if TimeZoneName != "America/Sao_Paulo" {
		t.Fatalf("TimeZoneName = %q", TimeZoneName)
	}
}

func TestDayStartIsBrasiliaMidnight(t *testing.T) {
	cases := []struct {
		at   time.Time
		want time.Time
	}{
		{at: time.Date(2026, 10, 21, 2, 59, 0, 0, time.UTC), want: time.Date(2026, 10, 20, 3, 0, 0, 0, time.UTC)},
		{at: time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC), want: time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC)},
		{at: time.Date(2026, 10, 21, 23, 0, 0, 0, time.UTC), want: time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		if got := DayStart(tc.at); !got.Equal(tc.want) || got.Location() != time.UTC {
			t.Errorf("DayStart(%v) = %v, want %v in UTC", tc.at, got, tc.want)
		}
	}
}
