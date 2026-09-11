package audience

import "time"

// How long a conversation must stay quiet before it is handed to the engine.
//
// This was a constant, and the wrong shape for one. A high-volume sales inbox
// wants a short window so a manager sees the verdict while the lead is still
// warm; a support desk whose conversations breathe over hours wants a long one,
// because analysing at five minutes of quiet just means analysing the same
// conversation again an hour later and paying twice. Neither is a default the
// product can pick, so it became a setting.
//
// Minutes, not seconds, because the sweep ticks once a minute: anything finer
// would be a number the system cannot honour.
const (
	// DefaultDebounceMinutes is the value every workspace ran under before this
	// was configurable, and is what an unset workspace still gets.
	DefaultDebounceMinutes = 5

	// MinDebounceMinutes is one tick of the sweep. Below this the window is not
	// shorter, it is just less predictable, since the job would decide on a
	// stamp it may or may not have seen yet.
	MinDebounceMinutes = 1

	// MaxDebounceMinutes is a day. Past that the conversation is not "settling",
	// it is over, and a workspace that wants analysis that late is better served
	// by the backstop than by a debounce window nobody remembers setting.
	MaxDebounceMinutes = 1440
)

// ClampDebounceMinutes resolves a stored value to the window actually used.
//
// Zero means "never set", which is every workspace until somebody changes it,
// and resolves to the default rather than to no wait at all. Out-of-range
// values are clamped rather than rejected here: this runs on the READ path, and
// a row edited by hand must not be able to stop the sweep. Rejection belongs on
// the write path, where there is a person to tell.
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

// DebounceWindow is the clamped value as a duration, for the sweep.
func DebounceWindow(minutes int) time.Duration {
	return time.Duration(ClampDebounceMinutes(minutes)) * time.Minute
}

// ValidDebounceMinutes reports whether a submitted value is one an operator may
// actually set. The write path uses this so a typo comes back as an error
// instead of being silently rounded into something they did not choose.
func ValidDebounceMinutes(minutes int) bool {
	return minutes >= MinDebounceMinutes && minutes <= MaxDebounceMinutes
}
