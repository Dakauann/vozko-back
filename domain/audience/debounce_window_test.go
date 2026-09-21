package audience

import (
	"testing"
	"time"
)

func TestClampDebounceMinutes(t *testing.T) {
	cases := []struct {
		name  string
		given int
		want  int
	}{
		{"unset falls back to the default", 0, DefaultDebounceMinutes},
		{"negative is treated as unset", -10, DefaultDebounceMinutes},
		{"a set value is honoured", 30, 30},
		{"the floor is one tick of the sweep", 0, DefaultDebounceMinutes},
		{"above the ceiling is clamped, not dropped", MaxDebounceMinutes + 500, MaxDebounceMinutes},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClampDebounceMinutes(tc.given); got != tc.want {
				t.Errorf("ClampDebounceMinutes(%d) = %d, want %d", tc.given, got, tc.want)
			}
		})
	}
}

func TestDebounceWindowMatchesTheOldConstant(t *testing.T) {
	if got := DebounceWindow(0); got != 5*time.Minute {
		t.Errorf("DebounceWindow(0) = %v, want the historical 5m", got)
	}
	if got := DebounceWindow(45); got != 45*time.Minute {
		t.Errorf("DebounceWindow(45) = %v, want 45m", got)
	}
}

func TestValidDebounceMinutesRejectsWhatTheWritePathMustNotAccept(t *testing.T) {
	for _, bad := range []int{0, -1, MaxDebounceMinutes + 1} {
		if ValidDebounceMinutes(bad) {
			t.Errorf("ValidDebounceMinutes(%d) = true, want false", bad)
		}
	}
	for _, ok := range []int{MinDebounceMinutes, DefaultDebounceMinutes, MaxDebounceMinutes} {
		if !ValidDebounceMinutes(ok) {
			t.Errorf("ValidDebounceMinutes(%d) = false, want true", ok)
		}
	}
}
