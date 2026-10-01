package shared

import (
	"testing"
	"time"
)

func TestADayBoundCoversTheWholeDayAndAnInstantIsKept(t *testing.T) {
	from := ParseDateBound("2026-09-01", false)
	to := ParseDateBound("2026-09-01", true)
	if from == nil || to == nil || !from.Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)) || to.Sub(*from) != 24*time.Hour-time.Nanosecond {
		t.Fatalf("from=%v to=%v", from, to)
	}
	instant := ParseDateBound("2026-09-01T10:30:00-03:00", true)
	if instant == nil || instant.Hour() != 10 {
		t.Fatalf("instant = %v", instant)
	}
	if ParseDateBound("", false) != nil || ParseDateBound("ontem", false) != nil {
		t.Fatal("blank or unreadable input must not become a bound")
	}
}
