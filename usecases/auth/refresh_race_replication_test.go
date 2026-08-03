package auth_usecase

// Replication of the production "stuck session" bug (2026-07): users randomly end
// up with every request 401ing, the avatar skeleton spinning forever, and a hard
// reload unable to reach /login; only a fresh login recovers. Both tests below
// drive Execute exactly as an HONEST browser does and end in handleReuse nuking
// the whole account (all sessions revoked + token version bumped), which is the
// stuck state. They pass against current code, i.e. they replicate the bug; a fix
// (true token-family lineage, or grace keyed per-token instead of per-session)
// must flip the ErrRefreshTokenReuse expectations to successful rotations.

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/auth"
)

// Scenario 1, concurrent refresh + Set-Cookie arrival order.
//
//	T0  tab A and tab B both hold refresh cookie "orig" and refresh concurrently
//	    (client single-flight can still let this happen: two browser profiles,
//	    a request already in flight when the lock is acquired, or a retry).
//	T0  A wins the CAS: session current=raw-1, previous=orig. A's response
//	    carries Set-Cookie refreshToken=raw-1.
//	T0  B loses the CAS, takes the grace path and re-rotates: current=raw-2,
//	    previous=raw-1. B's response carries Set-Cookie refreshToken=raw-2.
//	T0  The cookie jar keeps whichever Set-Cookie arrives LAST. With multiple
//	    replicas behind Cloudflare, A's response can land after B's, so the
//	    persisted cookie is raw-1, which the DB now considers spent.
//	T15m the access token expires; the browser refreshes with raw-1. It matches
//	    previous_refresh_token_hash, rotated_at is far outside the 30 s grace,
//	    and reuse detection fires: every session revoked, token version bumped.
func TestRefresh_ConcurrentRotationThenStaleCookieNukesHonestUser(t *testing.T) {
	userRepo, sessionRepo, issuer, shared := newRefreshFixture()

	// The user's phone is also signed in; it is collateral damage of the nuke.
	phone := &auth.Session{
		ID:               "sess-phone",
		UserID:           "u1",
		RefreshTokenHash: "hashed-phone",
		AccessJTI:        "jti-phone",
		ExpiresAt:        time.Now().Add(24 * time.Hour),
	}
	sessionRepo.sessions["sess-phone"] = phone
	sessionRepo.byHash["hashed-phone"] = phone

	uc := NewRefreshTokenUseCase(userRepo, issuer, sessionRepo, shared)

	// Tab A rotates orig -> raw-1.
	pairA, err := uc.Execute("orig", "", "")
	if err != nil {
		t.Fatalf("tab A rotation failed: %v", err)
	}
	// Tab B raced with the same cookie, lost the CAS, and grace-re-rotates
	// raw-1 -> raw-2. (Presenting "orig" post-rotation IS the CAS-loser path.)
	pairB, err := uc.Execute("orig", "", "")
	if err != nil {
		t.Fatalf("tab B grace re-rotation failed: %v", err)
	}
	if pairA.RefreshToken == pairB.RefreshToken {
		t.Fatal("test setup broken: both rotations returned the same token")
	}

	// A's Set-Cookie arrives last, so the jar persists pairA.RefreshToken,
	// now the DB's *previous* hash. 15 minutes pass (access-token TTL) before
	// the next refresh, far beyond the 30 s grace window.
	sessionRepo.sessions["sess1"].RotatedAt = timePtr(time.Now().Add(-2 * refreshGraceWindow))

	_, err = uc.Execute(pairA.RefreshToken, "", "")

	// Current behavior: the honest browser is treated as a thief.
	if !errors.Is(err, auth.ErrRefreshTokenReuse) {
		t.Fatalf("replication expected ErrRefreshTokenReuse (the bug), got: %v", err)
	}
	if !sessionRepo.sessions["sess1"].IsRevoked() {
		t.Error("expected the browser session to be revoked (bug symptom)")
	}
	if !sessionRepo.sessions["sess-phone"].IsRevoked() {
		t.Error("expected the phone session to be collaterally revoked (bug symptom)")
	}
	if userRepo.byID["u1"].TokenVersion == 0 {
		t.Error("expected token version bump: this is why Ctrl+Shift+R cannot recover")
	}
}

// Scenario 2, lost refresh response, no concurrency at all.
//
// The browser POSTs /auth/refresh; the server rotates orig -> raw-1, but the
// response never lands (tab closed mid-flight, navigation aborts the fetch,
// network drop, Cloudflare 5xx). The jar still holds "orig". The user comes
// back minutes later; the refresh presents "orig", outside grace -> nuke.
// The 30 s grace only covers an immediate retry, not a lost-response client
// that returns later, which is a routine event, not an attack.
func TestRefresh_LostResponseThenLaterRefreshNukesHonestUser(t *testing.T) {
	userRepo, sessionRepo, issuer, shared := newRefreshFixture()
	uc := NewRefreshTokenUseCase(userRepo, issuer, sessionRepo, shared)

	// Server rotates, but the client never receives Set-Cookie raw-1.
	if _, err := uc.Execute("orig", "", ""); err != nil {
		t.Fatalf("rotation failed: %v", err)
	}

	// The user returns after the grace window (e.g. laptop reopened).
	sessionRepo.sessions["sess1"].RotatedAt = timePtr(time.Now().Add(-2 * refreshGraceWindow))

	_, err := uc.Execute("orig", "", "")
	if !errors.Is(err, auth.ErrRefreshTokenReuse) {
		t.Fatalf("replication expected ErrRefreshTokenReuse (the bug), got: %v", err)
	}
	if userRepo.byID["u1"].TokenVersion == 0 {
		t.Error("expected token version bump: only a fresh login recovers")
	}
}
