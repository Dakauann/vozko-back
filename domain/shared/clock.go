package shared

import "time"

// Clock is the source of "now".
//
// Injected rather than called directly so time arithmetic (debounce
// windows, dispatch leases, rollup buckets) is testable without sleeping.
type Clock interface {
	Now() time.Time
}

// SystemClock is the real clock. Always UTC: every stored instant is UTC,
// and a local-time leak here would shift a delivery, or a daily bucket, by
// the server's offset.
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }

var _ Clock = SystemClock{}
