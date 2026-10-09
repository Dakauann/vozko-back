package shared

import "time"

type Lease struct {
	Claim       string
	HeartbeatAt *time.Time
	Attempts    int
}

func (l Lease) Free(now time.Time, staleAfter time.Duration) bool {
	return l.Claim == "" || l.HeartbeatAt == nil || l.HeartbeatAt.Before(now.Add(-staleAfter))
}

func (l Lease) Claimable(now time.Time, staleAfter time.Duration, maxAttempts int) bool {
	return l.Attempts < maxAttempts && l.Free(now, staleAfter)
}
