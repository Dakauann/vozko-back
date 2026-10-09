package shared

import (
	"strings"
	"time"
)

type Reservation struct {
	Holder string
	Until  time.Time
}

func ReserveFor(holder string, now time.Time, ttl time.Duration) Reservation {
	return Reservation{Holder: holder, Until: now.Add(ttl)}
}

func (r Reservation) Live(now time.Time) bool {
	return strings.TrimSpace(r.Holder) != "" && now.Before(r.Until)
}

func (r Reservation) HeldBy(holder string, now time.Time) bool {
	return strings.TrimSpace(holder) != "" && r.Holder == holder && r.Live(now)
}

func (r Reservation) TakeableBy(holder string, now time.Time) bool {
	if strings.TrimSpace(holder) == "" {
		return false
	}
	return !r.Live(now) || r.Holder == holder
}
