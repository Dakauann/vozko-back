package carriercampaign

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/carriermessaging"
	"vozko/domain/working_hours"
)

func compiled(t *testing.T, spec working_hours.Spec) *working_hours.Schedule {
	t.Helper()
	s, err := spec.Compile()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSendWindow(t *testing.T) {
	sp := mustLocation(t, saoPaulo)
	quiet := compiled(t, carriermessaging.DefaultQuietHours(saoPaulo))
	closed := working_hours.New(sp, map[time.Weekday][]working_hours.Interval{})

	cases := []struct {
		name    string
		kind    Kind
		quiet   *working_hours.Schedule
		now     time.Time
		sendNow bool
		next    time.Time
		err     error
	}{
		{"marketing inside the window", KindMarketing, quiet, time.Date(2026, 10, 21, 10, 0, 0, 0, sp), true, time.Date(2026, 10, 21, 10, 0, 0, 0, sp), nil},
		{"marketing at the opening", KindMarketing, quiet, time.Date(2026, 10, 21, 8, 0, 0, 0, sp), true, time.Date(2026, 10, 21, 8, 0, 0, 0, sp), nil},
		{"marketing before opening waits", KindMarketing, quiet, time.Date(2026, 10, 21, 6, 30, 0, 0, sp), false, time.Date(2026, 10, 21, 8, 0, 0, 0, sp), nil},
		{"marketing at closing waits for tomorrow", KindMarketing, quiet, time.Date(2026, 10, 21, 20, 0, 0, 0, sp), false, time.Date(2026, 10, 22, 8, 0, 0, 0, sp), nil},
		{"marketing late night waits for tomorrow", KindMarketing, quiet, time.Date(2026, 10, 21, 23, 15, 0, 0, sp), false, time.Date(2026, 10, 22, 8, 0, 0, 0, sp), nil},
		{"transactional sends at night", KindTransactional, quiet, time.Date(2026, 10, 21, 23, 15, 0, 0, sp), true, time.Date(2026, 10, 21, 23, 15, 0, 0, sp), nil},
		{"transactional needs no schedule", KindTransactional, nil, time.Date(2026, 10, 21, 3, 0, 0, 0, sp), true, time.Date(2026, 10, 21, 3, 0, 0, 0, sp), nil},
		{"marketing without quiet hours is refused", KindMarketing, nil, time.Date(2026, 10, 21, 10, 0, 0, 0, sp), false, time.Time{}, ErrQuietHoursRequired},
		{"never opening schedule is refused", KindMarketing, closed, time.Date(2026, 10, 21, 10, 0, 0, 0, sp), false, time.Time{}, working_hours.ErrNoOpenTime},
		{"unknown kind is refused", Kind("promo"), quiet, time.Date(2026, 10, 21, 10, 0, 0, 0, sp), false, time.Time{}, ErrKindInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sendNow, next, err := SendWindow(tc.kind, tc.quiet, tc.now)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if sendNow != tc.sendNow || !next.Equal(tc.next) {
				t.Fatalf("got (%v, %s), want (%v, %s)", sendNow, next, tc.sendNow, tc.next)
			}
		})
	}
}
