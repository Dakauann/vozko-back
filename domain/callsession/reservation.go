package callsession

import "time"

const CallSessionReservationTTL = 60 * time.Second

type ReservationState struct {
	token      string
	reservedAt time.Time
}

func (r *ReservationState) Reserve(token string, activeAlready bool, now time.Time, ttl time.Duration) bool {
	if token == "" || activeAlready {
		return false
	}
	if r.reservedLive(now, ttl) {
		return r.token == token
	}
	r.token = token
	r.reservedAt = now
	return true
}

func (r *ReservationState) Release(token string) {
	if token != "" && r.token == token {
		r.clear()
	}
}

func (r *ReservationState) Clear() { r.clear() }

func (r *ReservationState) ReservedLive(now time.Time, ttl time.Duration) bool {
	return r.reservedLive(now, ttl)
}

func (r *ReservationState) reservedLive(now time.Time, ttl time.Duration) bool {
	if r.token == "" {
		return false
	}
	if now.Sub(r.reservedAt) >= ttl {
		r.clear()
		return false
	}
	return true
}

func (r *ReservationState) clear() {
	r.token = ""
	r.reservedAt = time.Time{}
}
