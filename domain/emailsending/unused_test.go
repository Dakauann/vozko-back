package emailsending

import (
	"testing"
	"time"
)

func TestAnUnusedAccountIsWarnedAheadThenReleased(t *testing.T) {
	activated := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	warnedEarly := activated.Add(23 * day)
	warnedLate := activated.Add(40 * day)
	cases := []struct {
		name   string
		status AccountStatus
		used   bool
		warned *time.Time
		at     time.Duration
		want   UnusedStep
	}{
		{"fresh account", AccountActive, false, nil, 10 * day, UnusedKeep},
		{"a week before the deadline", AccountActive, false, nil, 23 * day, UnusedWarn},
		{"warned and still before the deadline", AccountActive, false, &warnedEarly, 29 * day, UnusedWarn},
		{"warned and past the deadline", AccountActive, false, &warnedEarly, 30 * day, UnusedRelease},
		{"never warned past the deadline gets the full notice first", AccountActive, false, nil, 60 * day, UnusedWarn},
		{"warned late keeps the full notice", AccountActive, false, &warnedLate, 45 * day, UnusedWarn},
		{"warned late is released after the notice", AccountActive, false, &warnedLate, 47 * day, UnusedRelease},
		{"a dormant account is released too", AccountDormant, false, &warnedEarly, 31 * day, UnusedRelease},
		{"an account that sent stays", AccountActive, true, &warnedEarly, 31 * day, UnusedKeep},
		{"a paused account is left to the admin", AccountPausedByPlatform, false, &warnedEarly, 31 * day, UnusedKeep},
		{"a paused account by the provider stays", AccountPausedByProvider, false, nil, 31 * day, UnusedKeep},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			acct := SenderAccount{Status: tc.status, CreatedAt: activated, UnusedWarnedAt: tc.warned}
			if got := UnusedStepFor(acct, tc.used, activated.Add(tc.at)); got != tc.want {
				t.Fatalf("step = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestTheReleaseDateIsThirtyDaysAfterActivationOrAWeekAfterTheWarning(t *testing.T) {
	activated := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	if got := UnusedReleaseAt(activated, nil); !got.Equal(activated.Add(30 * 24 * time.Hour)) {
		t.Fatalf("release at = %s", got)
	}
	early := activated.Add(23 * 24 * time.Hour)
	if got := UnusedReleaseAt(activated, &early); !got.Equal(activated.Add(30 * 24 * time.Hour)) {
		t.Fatalf("an early warning keeps the 30 days: %s", got)
	}
	late := activated.Add(50 * 24 * time.Hour)
	if got := UnusedReleaseAt(activated, &late); !got.Equal(late.Add(7 * 24 * time.Hour)) {
		t.Fatalf("a late warning gets its full notice: %s", got)
	}
	if UnusedReleaseAfter != 30*24*time.Hour || UnusedReleaseNotice != 7*24*time.Hour {
		t.Fatal("the owner's rule is 30 days with a week of notice")
	}
}
