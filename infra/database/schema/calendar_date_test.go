package schema

import (
	"testing"
	"time"
)

func TestCalendarDateWritesTheDayAsTextAndNullWhenEmpty(t *testing.T) {
	if value, err := CalendarDate("").Value(); err != nil || value != nil {
		t.Fatalf("an empty date must be NULL, got %v (%v)", value, err)
	}
	if value, err := CalendarDate("1990-04-21").Value(); err != nil || value != "1990-04-21" {
		t.Fatalf("a date must be written as its day, got %v (%v)", value, err)
	}
}

func TestCalendarDateReadsTheDayWhateverTheDriverHandsBack(t *testing.T) {
	saoPaulo := time.FixedZone("BRT", -3*3600)
	cases := []struct {
		name  string
		value interface{}
		want  CalendarDate
	}{
		{"null", nil, ""},
		{"a driver date at UTC midnight", time.Date(1990, time.April, 21, 0, 0, 0, 0, time.UTC), "1990-04-21"},
		{"a driver date in another zone keeps its own day", time.Date(1990, time.April, 21, 0, 0, 0, 0, saoPaulo), "1990-04-21"},
		{"text", "1990-04-21", "1990-04-21"},
		{"bytes", []byte("1990-04-21"), "1990-04-21"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got CalendarDate
			if err := got.Scan(tc.value); err != nil || got != tc.want {
				t.Fatalf("Scan(%v) = %q, %v; want %q", tc.value, got, err, tc.want)
			}
		})
	}
}

func TestCalendarDateRefusesWhatIsNotADay(t *testing.T) {
	for _, bad := range []interface{}{"21/04/1990", []byte("x"), 42} {
		var got CalendarDate
		if err := got.Scan(bad); err == nil {
			t.Errorf("Scan(%v) must fail, got %q", bad, got)
		}
	}
}
