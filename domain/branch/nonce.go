package branch

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

// NonceLifetime bounds how long an issued REGISTER nonce stays valid. It caps the
// replay window: a captured Authorization header can only be reused within this many
// seconds AND only from the same source IP. Matches Asterisk res_pjsip's default
// nonce_lifetime (RFC 7616 §5.4).
const NonceLifetime = 32 * time.Second

// NonceService issues and verifies stateless, signed digest nonces. A nonce is
// self-authenticating: it carries its own issue time and an HMAC over that time, the
// request source IP, and the realm. Verification recomputes the HMAC, so no
// server-side nonce table is needed and the check is identical on any replica that
// shares the secret. This mirrors Asterisk's res_pjsip_authenticator_digest, which
// derives the nonce from a timestamp + source + a server id + the realm and rejects
// any nonce it would not itself have produced.
//
// This replaces a predictable, unverified nonce (which let a captured REGISTER be
// replayed indefinitely, and, since the digest does not cover the Contact header,
// let an attacker rebind the AOR to their own phone). Binding the nonce to the source
// IP and a short lifetime is the standard mitigation.
type NonceService struct {
	secret []byte
	now    func() time.Time
}

// NewNonceService builds a service with an explicit secret (use in tests for
// deterministic nonces, or in prod to share one secret across replicas).
func NewNonceService(secret []byte, now func() time.Time) *NonceService {
	if now == nil {
		now = time.Now
	}
	return &NonceService{secret: secret, now: now}
}

// NewRandomNonceService builds a service with a fresh 32-byte random secret. On a
// single VPS this is ideal: a restart simply invalidates outstanding nonces (phones
// transparently re-challenge). For multi-replica, share one secret via config
// instead so a nonce issued by one replica verifies on another.
func NewRandomNonceService(now func() time.Time) *NonceService {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		// rand should never fail; fall back to a time-seeded secret so we never run
		// with an empty (forgeable) key.
		secret = []byte(time.Now().Format(time.RFC3339Nano) + "-vozko-branch-nonce")
	}
	return NewNonceService(secret, now)
}

// Issue returns a signed nonce bound to sourceIP and realm at the current time.
func (n *NonceService) Issue(sourceIP, realm string) string {
	ts := strconv.FormatInt(n.now().Unix(), 10)
	return ts + "." + n.sign(ts, sourceIP, realm)
}

// Verify reports whether nonce is one this service issued for sourceIP+realm and has
// not expired. It returns:
//   - ok=true                : valid signature, correct source, still fresh -> proceed.
//   - ok=false, stale=true   : valid signature but past NonceLifetime -> re-challenge
//     with stale=true so the phone retries WITHOUT re-prompting for the password.
//   - ok=false, stale=false  : forged, tampered, or issued for a different source
//     (a likely replay/relay attempt) -> re-challenge with a fresh nonce.
func (n *NonceService) Verify(nonce, sourceIP, realm string) (ok, stale bool) {
	ts, sig, found := strings.Cut(nonce, ".")
	if !found {
		return false, false
	}
	// Constant-time compare of the HMAC; also proves the nonce was issued for THIS
	// source IP and realm (both are inputs to the signature).
	if !hmac.Equal([]byte(sig), []byte(n.sign(ts, sourceIP, realm))) {
		return false, false
	}
	issued, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false, false
	}
	age := n.now().Sub(time.Unix(issued, 0))
	if age < 0 || age > NonceLifetime {
		return false, true // our nonce, but expired
	}
	return true, false
}

// sign is the HMAC over the nonce's fields. Fields are newline-separated; none of ts
// (digits), sourceIP, or realm contain a newline, so the encoding is unambiguous and
// bytes cannot be shifted between fields to forge a match.
func (n *NonceService) sign(ts, sourceIP, realm string) string {
	mac := hmac.New(sha256.New, n.secret)
	mac.Write([]byte(ts + "\n" + sourceIP + "\n" + realm))
	return hex.EncodeToString(mac.Sum(nil))
}
