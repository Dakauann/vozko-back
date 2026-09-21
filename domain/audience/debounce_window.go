package audience

import "time"

const (
	DefaultDebounceMinutes = 5

	MinDebounceMinutes = 1

	MaxDebounceMinutes = 1440
)

func ClampDebounceMinutes(minutes int) int {
	if minutes <= 0 {
		return DefaultDebounceMinutes
	}
	if minutes < MinDebounceMinutes {
		return MinDebounceMinutes
	}
	if minutes > MaxDebounceMinutes {
		return MaxDebounceMinutes
	}
	return minutes
}

func DebounceWindow(minutes int) time.Duration {
	return time.Duration(ClampDebounceMinutes(minutes)) * time.Minute
}

func ValidDebounceMinutes(minutes int) bool {
	return minutes >= MinDebounceMinutes && minutes <= MaxDebounceMinutes
}
