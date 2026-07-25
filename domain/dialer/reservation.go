package dialer

import "time"

// ReservationState is the shared "busy-while-ringing" bookkeeping used by every
// DialerSession implementation (the browser dialerSession and the SIP
// branchDialerSession) so the compare-and-set plus TTL-backstop logic lives in
// exactly ONE place instead of being copied per session type.
//
// It is CALLER-LOCKED: every method assumes the owning session already holds its
// own mutex, exactly like the *Locked helpers it was extracted from. Because the
// session passes its own "already has an attached call" state in as a predicate,
// Reserve stays atomic with attach under the session's single lock, with no
// second lock and no observable free gap.
//
// DialerReservationTTL bounds how long a reservation can survive without an
// explicit Release. It is a pure backstop: every offer path releases on accept,
// decline, timeout and disconnect, so this only prevents a permanent leak if a
// release is ever missed. It MUST exceed the longest single-agent ring window
// (SIP inbound 30s, transfer 30s, WhatsApp per-agent 15s).
const DialerReservationTTL = 60 * time.Second

type ReservationState struct {
	token      string
	reservedAt time.Time
}

// Reserve is a compare-and-set: it fails (returns false) if the session already
// has an attached call (activeAlready) or a live reservation for a DIFFERENT
// token, so two concurrent offers can never both claim the same idle agent.
// Reserving with the same token again is idempotent and returns true. An empty
// token is rejected. Caller holds the session lock.
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

// Release clears a reservation held under the same token. Token-scoped and
// idempotent: releasing a stale/foreign/empty token is a no-op. Caller holds the lock.
func (r *ReservationState) Release(token string) {
	if token != "" && r.token == token {
		r.clear()
	}
}

// Clear unconditionally drops any outstanding reservation. Used when attach
// consumes the reservation and on session shutdown. Caller holds the lock.
func (r *ReservationState) Clear() { r.clear() }

// ReservedLive reports whether a non-expired reservation is held, lazily clearing
// one that has outlived the TTL backstop. Caller holds the lock.
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
