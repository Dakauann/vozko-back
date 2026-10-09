package balance

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/billing"
)

func int64Ptr(v int64) *int64 { return &v }

func TestNewMonthlySendCap_RejectsNonPositiveLimits(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	for _, limit := range []int64{0, -1} {
		if _, err := NewMonthlySendCap("ws-1", limit, 1, "admin-1", now); !errors.Is(err, ErrInvalidSendCapLimit) {
			t.Errorf("limit %d: want ErrInvalidSendCapLimit, got %v", limit, err)
		}
	}
	if _, err := NewMonthlySendCap(" ", 10, 1, "admin-1", now); !errors.Is(err, ErrSendCapWorkspaceRequired) {
		t.Errorf("blank workspace: want ErrSendCapWorkspaceRequired, got %v", err)
	}
	cap, err := NewMonthlySendCap("ws-1", 10, 15, "admin-1", now)
	if err != nil {
		t.Fatalf("valid cap: %v", err)
	}
	if cap.WorkspaceID != "ws-1" || cap.Limit != 10 || cap.CycleDay != 15 || cap.UpdatedBy != "admin-1" || !cap.UpdatedAt.Equal(now) {
		t.Errorf("unexpected cap %+v", cap)
	}
}

func TestSendCapCycleStart_FromDayOneIsTheSaoPauloCalendarMonth(t *testing.T) {
	brt := billing.LocationBRT()
	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"mid month", time.Date(2026, 9, 15, 10, 0, 0, 0, brt), time.Date(2026, 9, 1, 0, 0, 0, 0, brt)},
		{"last minute of the month in BRT", time.Date(2026, 9, 30, 23, 59, 0, 0, brt), time.Date(2026, 9, 1, 0, 0, 0, 0, brt)},
		{"already next month in UTC but not in BRT", time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC), time.Date(2026, 9, 1, 0, 0, 0, 0, brt)},
		{"first instant of the month in BRT", time.Date(2026, 10, 1, 0, 0, 0, 0, brt), time.Date(2026, 10, 1, 0, 0, 0, 0, brt)},
		{"january rolls the year", time.Date(2027, 1, 5, 8, 0, 0, 0, brt), time.Date(2027, 1, 1, 0, 0, 0, 0, brt)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SendCapCycleStart(tc.now, 1); !got.Equal(tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMonthlySendCap_CheckRoom(t *testing.T) {
	cap := MonthlySendCap{Limit: 3}
	cases := []struct {
		used    int64
		wantErr bool
	}{
		{0, false},
		{2, false},
		{3, true},
		{4, true},
		{-1, false},
	}
	for _, tc := range cases {
		err := cap.CheckRoom(tc.used)
		if tc.wantErr && !errors.Is(err, ErrMonthlySendCapReached) {
			t.Errorf("used %d: want ErrMonthlySendCapReached, got %v", tc.used, err)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("used %d: want admitted, got %v", tc.used, err)
		}
	}
}

func TestSendCapChangeRequiresUnlock(t *testing.T) {
	existing := &MonthlySendCap{WorkspaceID: "ws-1", Limit: 100, CycleDay: 1}
	with := func(limit int64, day int) *MonthlySendCap { return &MonthlySendCap{WorkspaceID: "ws-1", Limit: limit, CycleDay: day} }
	cases := []struct {
		name    string
		current *MonthlySendCap
		next    *MonthlySendCap
		want    bool
	}{
		{"creating a cap locks, never unlocks", nil, with(100, 15), false},
		{"lowering tightens the lock", existing, with(50, 1), false},
		{"keeping the same limit changes nothing", existing, with(100, 1), false},
		{"raising loosens the lock", existing, with(101, 1), true},
		{"moving the cycle day can restart the count", existing, with(100, 15), true},
		{"moving the cycle day while lowering still can restart the count", existing, with(10, 15), true},
		{"removing the cap loosens the lock", existing, nil, true},
		{"removing a missing cap loosens nothing", nil, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SendCapChangeRequiresUnlock(tc.current, tc.next); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSendCapUsage_RemainingAndLevel(t *testing.T) {
	cases := []struct {
		name          string
		limit, used   int64
		wantRemaining int64
		wantLevel     SendCapLevel
	}{
		{"untouched", 100, 0, 100, SendCapLevelOK},
		{"just under the near threshold", 100, 79, 21, SendCapLevelOK},
		{"at the near threshold", 100, 80, 20, SendCapLevelNear},
		{"one short of the limit", 100, 99, 1, SendCapLevelNear},
		{"exactly at the limit", 100, 100, 0, SendCapLevelReached},
		{"cap lowered below usage", 100, 130, 0, SendCapLevelReached},
		{"refunds outweigh sends", 100, -2, 102, SendCapLevelOK},
		{"threshold rounds up on small limits", 3, 2, 1, SendCapLevelOK},
		{"small limit near", 5, 4, 1, SendCapLevelNear},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			usage := SendCapUsage{Cap: MonthlySendCap{Limit: tc.limit}, Used: tc.used}
			if got := usage.Remaining(); got != tc.wantRemaining {
				t.Errorf("remaining: got %d, want %d", got, tc.wantRemaining)
			}
			if got := usage.Level(); got != tc.wantLevel {
				t.Errorf("level: got %q, want %q", got, tc.wantLevel)
			}
		})
	}
}

func TestParseSendCapLevel(t *testing.T) {
	cases := []struct {
		raw     string
		want    SendCapLevel
		wantErr bool
	}{
		{"", "", false},
		{"  ", "", false},
		{"ok", SendCapLevelOK, false},
		{"NEAR", SendCapLevelNear, false},
		{" reached ", SendCapLevelReached, false},
		{"full", "", true},
	}
	for _, tc := range cases {
		got, err := ParseSendCapLevel(tc.raw)
		if tc.wantErr {
			if !errors.Is(err, ErrInvalidSendCapLevel) {
				t.Errorf("%q: want ErrInvalidSendCapLevel, got %v", tc.raw, err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%q: got %q, %v; want %q", tc.raw, got, err, tc.want)
		}
	}
}

func TestSendCapUsage_MatchesLevel(t *testing.T) {
	near := SendCapUsage{Cap: MonthlySendCap{Limit: 10}, Used: 9}
	if !near.Matches("") {
		t.Error("an empty filter matches every level")
	}
	if !near.Matches(SendCapLevelNear) {
		t.Error("a near usage matches the near filter")
	}
	if near.Matches(SendCapLevelReached) {
		t.Error("a near usage does not match the reached filter")
	}
}

func TestVerifySendCapUnlockCode_EachAdminUsesTheirOwnCode(t *testing.T) {
	cases := []struct {
		name    string
		email   string
		code    string
		wantErr bool
	}{
		{"first admin with own code", "dakauannc@gmail.com", "1601", false},
		{"second admin with own code", "joscelioapinheiro@gmail.com", "9412", false},
		{"first admin with the second admin's code", "dakauannc@gmail.com", "9412", true},
		{"second admin with the first admin's code", "joscelioapinheiro@gmail.com", "1601", true},
		{"a right code from someone off the list", "ops@vozkoia.com", "1601", true},
		{"empty code", "dakauannc@gmail.com", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := VerifySendCapUnlockCode(tc.email, tc.code)
			if tc.wantErr && !errors.Is(err, ErrInvalidUnlockCode) {
				t.Fatalf("want ErrInvalidUnlockCode, got %v", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("want accepted, got %v", err)
			}
		})
	}
}

func TestMonthlySendCap_Unlocked(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	cap := MonthlySendCap{WorkspaceID: "ws-1", Limit: 100, UpdatedBy: "admin-1"}
	raised, err := cap.Unlocked(250, "root-1", now)
	if err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if raised.Limit != 250 || raised.UpdatedBy != "root-1" || raised.UnlockedBy == nil || *raised.UnlockedBy != "root-1" || raised.UnlockedAt == nil || !raised.UnlockedAt.Equal(now) {
		t.Errorf("unexpected unlocked cap %+v", raised)
	}
	if _, err := cap.Unlocked(0, "root-1", now); !errors.Is(err, ErrInvalidSendCapLimit) {
		t.Errorf("want ErrInvalidSendCapLimit, got %v", err)
	}
}

func TestMonthlySendCap_RelimitedKeepsUnlockHistory(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	root := "root-1"
	unlockedAt := now.Add(-time.Hour)
	cap := MonthlySendCap{WorkspaceID: "ws-1", Limit: 100, UnlockedBy: &root, UnlockedAt: &unlockedAt}
	lowered, err := cap.Relimited(40, "admin-2", now)
	if err != nil {
		t.Fatalf("relimit: %v", err)
	}
	if lowered.Limit != 40 || lowered.UpdatedBy != "admin-2" || !lowered.UpdatedAt.Equal(now) {
		t.Errorf("unexpected relimited cap %+v", lowered)
	}
	if lowered.UnlockedBy != &root || lowered.UnlockedAt != &unlockedAt {
		t.Error("relimiting must keep who unlocked last")
	}
	if _, err := cap.Relimited(-5, "admin-2", now); !errors.Is(err, ErrInvalidSendCapLimit) {
		t.Errorf("want ErrInvalidSendCapLimit, got %v", err)
	}
}

func TestSendCapActor_Permissions(t *testing.T) {
	cases := []struct {
		name       string
		actor      SendCapActor
		wantManage bool
		wantUnlock bool
	}{
		{"system admin on the allowlist", SendCapActor{UserID: "u-1", Email: "dakauannc@gmail.com", SystemAdmin: true}, true, true},
		{"second allowlisted system admin", SendCapActor{UserID: "u-2", Email: "joscelioapinheiro@gmail.com", SystemAdmin: true}, true, true},
		{"system admin off the allowlist", SendCapActor{UserID: "u-3", Email: "ops@vozkoia.com", SystemAdmin: true}, true, false},
		{"the old second address is no longer on the list", SendCapActor{UserID: "u-6", Email: "dakauannc@vozkoia.com", SystemAdmin: true}, true, false},
		{"allowlisted email without the system admin role", SendCapActor{UserID: "u-4", Email: "dakauannc@gmail.com"}, false, false},
		{"system admin without a user id", SendCapActor{Email: "dakauannc@gmail.com", SystemAdmin: true}, false, false},
		{"regular user", SendCapActor{UserID: "u-5", Email: "someone@acme.com"}, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.actor.CanManage(); got != tc.wantManage {
				t.Errorf("CanManage = %v, want %v", got, tc.wantManage)
			}
			if got := tc.actor.CanUnlock(); got != tc.wantUnlock {
				t.Errorf("CanUnlock = %v, want %v", got, tc.wantUnlock)
			}
		})
	}
}

func TestSortSendCapUsageByPressure(t *testing.T) {
	usages := []SendCapUsage{
		{WorkspaceName: "Calm", Cap: MonthlySendCap{WorkspaceID: "a", Limit: 1000}, Used: 10},
		{WorkspaceName: "Full", Cap: MonthlySendCap{WorkspaceID: "b", Limit: 10}, Used: 12},
		{WorkspaceName: "Near", Cap: MonthlySendCap{WorkspaceID: "c", Limit: 100}, Used: 90},
		{WorkspaceName: "Also near", Cap: MonthlySendCap{WorkspaceID: "d", Limit: 200}, Used: 180},
	}
	SortSendCapUsageByPressure(usages)
	got := []string{}
	for _, u := range usages {
		got = append(got, u.WorkspaceName)
	}
	want := []string{"Full", "Also near", "Near", "Calm"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestNewMonthlySendCap_RejectsADayOutsideTheMonth(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	for _, day := range []int{0, -1, 32} {
		if _, err := NewMonthlySendCap("ws-1", 10, day, "admin-1", now); !errors.Is(err, ErrInvalidSendCapCycleDay) {
			t.Errorf("day %d: want ErrInvalidSendCapCycleDay, got %v", day, err)
		}
	}
	for _, day := range []int{1, 15, 28, 31} {
		if _, err := NewMonthlySendCap("ws-1", 10, day, "admin-1", now); err != nil {
			t.Errorf("day %d is valid, got %v", day, err)
		}
	}
}

func TestSendCapCycleStart_RunsFromTheChosenDayToTheSameDayNextMonth(t *testing.T) {
	brt := billing.LocationBRT()
	at := func(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, brt) }
	cases := []struct {
		name string
		now  time.Time
		day  int
		want time.Time
	}{
		{"on the day itself", at(2026, 10, 15, 9), 15, at(2026, 10, 15, 0)},
		{"after the day", at(2026, 10, 20, 9), 15, at(2026, 10, 15, 0)},
		{"before the day belongs to last month's cycle", at(2026, 10, 14, 23), 15, at(2026, 9, 15, 0)},
		{"before the day in january goes back a year", at(2027, 1, 10, 9), 15, at(2026, 12, 15, 0)},
		{"day 31 in a 30 day month starts on the 30th", at(2026, 9, 30, 9), 31, at(2026, 9, 30, 0)},
		{"day 31 in february starts on the last day", at(2026, 2, 28, 9), 31, at(2026, 2, 28, 0)},
		{"day 31 early in march still belongs to february's cycle", at(2026, 3, 20, 9), 31, at(2026, 2, 28, 0)},
		{"day 31 at the end of march", at(2026, 3, 31, 1), 31, at(2026, 3, 31, 0)},
		{"day 29 in a leap february", at(2028, 2, 29, 9), 29, at(2028, 2, 29, 0)},
		{"already the 15th in UTC but still the 14th in BRT", time.Date(2026, 10, 15, 2, 0, 0, 0, time.UTC), 15, at(2026, 9, 15, 0)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SendCapCycleStart(tc.now, tc.day); !got.Equal(tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMonthlySendCap_NextCycleStart(t *testing.T) {
	brt := billing.LocationBRT()
	at := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, brt) }
	cases := []struct {
		name string
		day  int
		now  time.Time
		want time.Time
	}{
		{"day 15 renews next month", 15, at(2026, 10, 20), at(2026, 11, 15)},
		{"day 15 before the day renews this month", 15, at(2026, 10, 3), at(2026, 10, 15)},
		{"day 31 in january renews on the last day of february", 31, at(2026, 1, 31), at(2026, 2, 28)},
		{"day 1 in december renews in january", 1, at(2026, 12, 9), at(2027, 1, 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cap := MonthlySendCap{Limit: 10, CycleDay: tc.day}
			if got := cap.NextCycleStart(tc.now); !got.Equal(tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
			if !cap.CycleStart(tc.now).Before(cap.NextCycleStart(tc.now)) {
				t.Error("a cycle must end after it starts")
			}
		})
	}
}

func TestMonthlySendCap_ACapWithoutADayCountsFromTheFirst(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, billing.LocationBRT())
	if got := (MonthlySendCap{Limit: 10}).CycleStart(now); !got.Equal(SendCapCycleStart(now, 1)) {
		t.Errorf("got %v, want the first of the month", got)
	}
}

func TestMonthlySendCap_WindowEnd(t *testing.T) {
	brt := billing.LocationBRT()
	at := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, brt) }
	cases := []struct {
		name     string
		cycleDay int
		endDay   int
		now      time.Time
		want     time.Time
	}{
		{"no end day lasts the whole cycle", 15, 0, at(2026, 10, 20), at(2026, 11, 15)},
		{"from day 15 to day 20 closes after the 20th", 15, 20, at(2026, 10, 16), at(2026, 10, 21)},
		{"from day 25 to day 5 wraps into the next month", 25, 5, at(2026, 10, 30), at(2026, 11, 6)},
		{"an end day past the month closes on its last day", 1, 31, at(2026, 2, 10), at(2026, 3, 1)},
		{"an end day after the next cycle start is cut at the next cycle", 31, 30, at(2026, 1, 31), at(2026, 2, 28)},
		{"the same start and end day is a single day", 10, 10, at(2026, 10, 10), at(2026, 10, 11)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cap := MonthlySendCap{Limit: 10, CycleDay: tc.cycleDay, EndDay: tc.endDay}
			if got := cap.WindowEnd(tc.now); !got.Equal(tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMonthlySendCap_CheckWindow(t *testing.T) {
	brt := billing.LocationBRT()
	cap := MonthlySendCap{Limit: 10, CycleDay: 15, EndDay: 20}
	open := []time.Time{
		time.Date(2026, 10, 15, 0, 0, 0, 0, brt),
		time.Date(2026, 10, 20, 23, 59, 59, 0, brt),
	}
	for _, now := range open {
		if err := cap.CheckWindow(now); err != nil {
			t.Errorf("%v: want open, got %v", now, err)
		}
	}
	closed := []time.Time{
		time.Date(2026, 10, 21, 0, 0, 0, 0, brt),
		time.Date(2026, 11, 14, 23, 59, 59, 0, brt),
	}
	for _, now := range closed {
		if err := cap.CheckWindow(now); !errors.Is(err, ErrSendWindowClosed) || SendCapRefusal(err) != ErrSendWindowClosed {
			t.Errorf("%v: want ErrSendWindowClosed, got %v", now, err)
		}
	}
}

func TestMonthlySendCap_Rewindowed(t *testing.T) {
	cap := MonthlySendCap{Limit: 10, CycleDay: 15}
	for _, day := range []int{-1, 32} {
		if _, err := cap.Rewindowed(day); !errors.Is(err, ErrInvalidSendCapEndDay) {
			t.Errorf("end day %d: want ErrInvalidSendCapEndDay, got %v", day, err)
		}
	}
	next, err := cap.Rewindowed(20)
	if err != nil || next.EndDay != 20 {
		t.Fatalf("got %+v, %v", next, err)
	}
	if !SendCapChangeRequiresUnlock(&cap, &next) {
		t.Error("changing the window must require an unlock")
	}
}
