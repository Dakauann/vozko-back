package branch

import (
	"testing"
	"time"
)

func nonceSvcAt(t time.Time) *NonceService {
	return NewNonceService([]byte("unit-test-secret"), func() time.Time { return t })
}

func TestNonce_RoundTrip(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	svc := nonceSvcAt(base)
	n := svc.Issue("203.0.113.7", "vozko")

	ok, stale := svc.Verify(n, "203.0.113.7", "vozko")
	if !ok || stale {
		t.Fatalf("fresh nonce: ok/stale = %v/%v, want true/false", ok, stale)
	}
}

func TestNonce_ExpiredIsStale(t *testing.T) {
	issued := nonceSvcAt(time.Unix(1_700_000_000, 0)).Issue("203.0.113.7", "vozko")
	// Verify NonceLifetime+1s later.
	later := nonceSvcAt(time.Unix(1_700_000_000, 0).Add(NonceLifetime + time.Second))

	ok, stale := later.Verify(issued, "203.0.113.7", "vozko")
	if ok || !stale {
		t.Fatalf("expired nonce: ok/stale = %v/%v, want false/true", ok, stale)
	}
}

func TestNonce_WrongSourceRejected(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	svc := nonceSvcAt(base)
	n := svc.Issue("203.0.113.7", "vozko")

	// Different source IP -> not our nonce for this source, and NOT merely stale.
	if ok, stale := svc.Verify(n, "198.51.100.9", "vozko"); ok || stale {
		t.Fatalf("cross-source: ok/stale = %v/%v, want false/false", ok, stale)
	}
	// Different realm -> same.
	if ok, stale := svc.Verify(n, "203.0.113.7", "other"); ok || stale {
		t.Fatalf("cross-realm: ok/stale = %v/%v, want false/false", ok, stale)
	}
}

func TestNonce_TamperedRejected(t *testing.T) {
	svc := nonceSvcAt(time.Unix(1_700_000_000, 0))

	for _, bad := range []string{
		"",                     // empty
		"no-dot-separator",     // malformed
		"1700000000.deadbeef",  // right shape, wrong signature
		"notatimestamp.abc123", // non-numeric ts
	} {
		if ok, stale := svc.Verify(bad, "203.0.113.7", "vozko"); ok || stale {
			t.Fatalf("tampered %q: ok/stale = %v/%v, want false/false", bad, ok, stale)
		}
	}
}

func TestNonce_DifferentSecretsDoNotVerify(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	a := NewNonceService([]byte("secret-a"), func() time.Time { return base })
	b := NewNonceService([]byte("secret-b"), func() time.Time { return base })

	n := a.Issue("203.0.113.7", "vozko")
	if ok, _ := b.Verify(n, "203.0.113.7", "vozko"); ok {
		t.Fatal("a nonce signed with secret-a must not verify under secret-b")
	}
}
