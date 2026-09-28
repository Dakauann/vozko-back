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
		if _, err := NewMonthlySendCap("ws-1", limit, "admin-1", now); !errors.Is(err, ErrInvalidSendCapLimit) {
			t.Errorf("limit %d: want ErrInvalidSendCapLimit, got %v", limit, err)
		}
	}
	if _, err := NewMonthlySendCap(" ", 10, "admin-1", now); !errors.Is(err, ErrSendCapWorkspaceRequired) {
		t.Errorf("blank workspace: want ErrSendCapWorkspaceRequired, got %v", err)
	}
	cap, err := NewMonthlySendCap("ws-1", 10, "admin-1", now)
	if err != nil {
		t.Fatalf("valid cap: %v", err)
	}
	if cap.WorkspaceID != "ws-1" || cap.Limit != 10 || cap.UpdatedBy != "admin-1" || !cap.UpdatedAt.Equal(now) {
		t.Errorf("unexpected cap %+v", cap)
	}
}

func TestSendCapMonthStart_UsesSaoPauloCalendarMonth(t *testing.T) {
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
			if got := SendCapMonthStart(tc.now); !got.Equal(tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMonthlySendCap_GuardCarriesLimitAndMonthStart(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	cap := MonthlySendCap{WorkspaceID: "ws-1", Limit: 500}
	guard := cap.Guard(now)
	if guard.Limit != 500 || !guard.Since.Equal(SendCapMonthStart(now)) {
		t.Errorf("unexpected guard %+v", guard)
	}
}

func TestMonthlySendCapGuard_Admit(t *testing.T) {
	guard := MonthlySendCapGuard{Limit: 3}
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
		err := guard.Admit(tc.used)
		if tc.wantErr && !errors.Is(err, ErrMonthlySendCapReached) {
			t.Errorf("used %d: want ErrMonthlySendCapReached, got %v", tc.used, err)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("used %d: want admitted, got %v", tc.used, err)
		}
	}
}

func TestSendCapChangeRequiresUnlock(t *testing.T) {
	existing := &MonthlySendCap{WorkspaceID: "ws-1", Limit: 100}
	cases := []struct {
		name     string
		current  *MonthlySendCap
		newLimit *int64
		want     bool
	}{
		{"creating a cap locks, never unlocks", nil, int64Ptr(100), false},
		{"lowering tightens the lock", existing, int64Ptr(50), false},
		{"keeping the same limit changes nothing", existing, int64Ptr(100), false},
		{"raising loosens the lock", existing, int64Ptr(101), true},
		{"removing the cap loosens the lock", existing, nil, true},
		{"removing a missing cap loosens nothing", nil, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SendCapChangeRequiresUnlock(tc.current, tc.newLimit); got != tc.want {
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

func TestVerifySendCapUnlockCode(t *testing.T) {
	if err := VerifySendCapUnlockCode(sendCapUnlockCode); err != nil {
		t.Errorf("the configured code must verify, got %v", err)
	}
	if err := VerifySendCapUnlockCode(" " + sendCapUnlockCode + " "); err != nil {
		t.Errorf("surrounding spaces are ignored, got %v", err)
	}
	for _, code := range []string{"", "0000", sendCapUnlockCode + "0", sendCapUnlockCode[:3]} {
		if code == sendCapUnlockCode {
			continue
		}
		if err := VerifySendCapUnlockCode(code); !errors.Is(err, ErrInvalidUnlockCode) {
			t.Errorf("%q: want ErrInvalidUnlockCode, got %v", code, err)
		}
	}
}

func TestSendCapUnlockCode_IsFourDigits(t *testing.T) {
	if len(sendCapUnlockCode) != 4 {
		t.Fatalf("unlock code must have 4 digits, has %d", len(sendCapUnlockCode))
	}
	for _, r := range sendCapUnlockCode {
		if r < '0' || r > '9' {
			t.Fatalf("unlock code must be numeric, got %q", sendCapUnlockCode)
		}
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
		{"second allowlisted system admin", SendCapActor{UserID: "u-2", Email: "dakauannc@vozkoia.com", SystemAdmin: true}, true, true},
		{"system admin off the allowlist", SendCapActor{UserID: "u-3", Email: "ops@vozkoia.com", SystemAdmin: true}, true, false},
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
