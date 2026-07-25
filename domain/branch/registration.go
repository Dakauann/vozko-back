package branch

import (
	"crypto/md5"
	"encoding/hex"
	"strings"
	"time"
)

// RegistrationBinding is one live "AOR contact": where a registered phone can be
// reached right now. It is the equivalent of an Asterisk ps_contacts row. On a
// single VPS these live in an in-process store (see infra), refreshed by
// re-REGISTER and evicted on expiry.
//
// ReceivedFrom is the real UDP source ip:port the REGISTER arrived from (rport,
// RFC 3581). ALL subsequent requests toward the phone route there, NOT to the
// phone's Contact host, which is usually a private/NATed address. This
// signaling-side rewrite_contact is the single most important NAT correctness
// rule; without it inbound calls to a NATed phone never arrive.
type RegistrationBinding struct {
	SIPUser      string
	BranchID     string
	WorkspaceID  string
	UserID       string // the member the branch belongs to (needed to rehydrate routing)
	Contact      string // Contact URI exactly as the phone sent it
	ReceivedFrom string // real source ip:port (rewrite_contact target)
	Transport    string
	CallID       string
	CSeq         int
	UserAgent    string
	CreatedAt    time.Time
	ExpiresAt    time.Time
}

func (b RegistrationBinding) Expired(now time.Time) bool {
	return !b.ExpiresAt.After(now)
}

// BindingStore holds the live AOR bindings. On a single VPS this is an in-process
// map with TTL eviction; the port is defined here so a durable (Postgres/Redis)
// implementation can replace it later without touching the use cases.
type BindingStore interface {
	// Upsert inserts or refreshes the binding for (sip_user, call_id). Idempotent
	// re-REGISTER updates the existing row (bumps expiry/cseq) rather than
	// duplicating it.
	Upsert(b RegistrationBinding) error
	// Remove drops the binding for (sip_user, call_id) (an Expires: 0 de-register).
	Remove(sipUser, callID string) error
	// ListLive returns the non-expired bindings for a sip_user (the contacts to
	// ring). Implementations must not return expired rows.
	ListLive(sipUser string) ([]RegistrationBinding, error)
	// CountLive is used to enforce max_contacts.
	CountLive(sipUser string) (int, error)
	// ReapExpired evicts everything at or past its expiry and returns the evicted
	// bindings so the reaper can take a branch offline (OnBranchUnreachable) once its
	// last live contact has lapsed.
	ReapExpired(now time.Time) []RegistrationBinding
}

// Registration expiry bounds (seconds). A phone asking below the floor gets a 423
// Interval Too Brief; above the ceiling is clamped down (the phone MUST honor the
// server-granted Expires, RFC 3261 §10.2.4). The floor is kept short so a re-REGISTER
// refreshes the NAT pinhole before it times out (UDP mappings close in ~30 to 60s).
//
// The ceiling is deliberately low (not the RFC/Asterisk 3600 default): with an
// in-process, non-durable location store and no OPTIONS "qualify" probing yet, the
// registration interval is ALSO our liveness and NAT-keepalive signal. Capping it at
// 2 min means a phone that dies, or the server restarting, self-corrects within one
// refresh instead of lingering "online" for up to an hour. Raise this only once a
// durable store + qualify decouple liveness from the registration period.
const (
	MinExpiresSeconds     = 30
	MaxExpiresSeconds     = 120
	DefaultExpiresSeconds = 60
)

// ClampExpires returns the granted expiry and whether the request was below the
// floor (which the registrar answers with 423 + Min-Expires).
func ClampExpires(requested int) (granted int, tooBrief bool) {
	if requested < 0 {
		return DefaultExpiresSeconds, false
	}
	if requested == 0 {
		return 0, false // de-register
	}
	if requested < MinExpiresSeconds {
		return MinExpiresSeconds, true
	}
	if requested > MaxExpiresSeconds {
		return MaxExpiresSeconds, false
	}
	return requested, false
}

// --- digest auth (HA1 based, no plaintext) -------------------------------

func md5hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// ComputeDigestResponse derives the expected RFC 2617 digest response from the
// stored HA1 (never the plaintext password). qop is "" for legacy (response =
// MD5(HA1:nonce:HA2)) or "auth" (response = MD5(HA1:nonce:nc:cnonce:qop:HA2)),
// where HA2 = MD5(method:uri). Verification compares this against the phone's
// Authorization response in constant time is unnecessary (it is a hash compare),
// but callers should still avoid leaking which field mismatched.
func ComputeDigestResponse(ha1, method, uri, nonce, qop, nc, cnonce string) string {
	ha2 := md5hex(strings.ToUpper(method) + ":" + uri)
	if qop == "" {
		return md5hex(ha1 + ":" + nonce + ":" + ha2)
	}
	return md5hex(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
}

// VerifyDigest reports whether the phone's response matches the one derived from
// the stored HA1 for the challenged nonce.
func VerifyDigest(ha1, method, uri, nonce, qop, nc, cnonce, clientResponse string) bool {
	if ha1 == "" || clientResponse == "" {
		return false
	}
	want := ComputeDigestResponse(ha1, method, uri, nonce, qop, nc, cnonce)
	return strings.EqualFold(want, strings.TrimSpace(clientResponse))
}
