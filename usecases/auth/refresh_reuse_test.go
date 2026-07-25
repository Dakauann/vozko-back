package auth_usecase

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"vozko/domain/auth"
	"vozko/domain/user"
)

// seqTokenIssuer hands out a distinct refresh token and access JTI on every call
// so a test can tell one rotation apart from the next (the shared testTokenIssuer
// returns fixed values, which would make two rotations indistinguishable).
type seqTokenIssuer struct {
	refreshN int
	jtiN     int
}

func (t *seqTokenIssuer) Issue(u *user.User) (*auth.TokenPair, error) {
	t.jtiN++
	return &auth.TokenPair{
		AccessToken: "at",
		UserID:      u.ID,
		Email:       u.Email,
		Role:        string(u.Role),
		AccessJTI:   fmt.Sprintf("jti-%d", t.jtiN),
	}, nil
}
func (t *seqTokenIssuer) GenerateRefreshToken() (string, string, error) {
	t.refreshN++
	raw := fmt.Sprintf("raw-%d", t.refreshN)
	return raw, "hashed-" + raw, nil
}
func (t *seqTokenIssuer) HashRefreshToken(raw string) string { return "hashed-" + raw }

func timePtr(tm time.Time) *time.Time { return &tm }

// newRefreshFixture sets up a user with one live session holding the token "orig".
func newRefreshFixture() (*testUserRepo, *testSessionRepo, *seqTokenIssuer, *testSharedState) {
	userRepo := newTestUserRepo()
	userRepo.byID["u1"] = &user.User{ID: "u1", Email: "u@t.com", Role: user.RoleUser}

	sessionRepo := newTestSessionRepo()
	issuer := &seqTokenIssuer{}
	sess := &auth.Session{
		ID:               "sess1",
		UserID:           "u1",
		RefreshTokenHash: issuer.HashRefreshToken("orig"),
		AccessJTI:        "jti-orig",
		ExpiresAt:        time.Now().Add(24 * time.Hour),
	}
	sessionRepo.sessions["sess1"] = sess
	sessionRepo.byHash[sess.RefreshTokenHash] = sess

	return userRepo, sessionRepo, issuer, newTestSharedState()
}

// A normal rotation must record where it rotated from, which is the data reuse and
// grace handling depend on.
func TestRefresh_RotationRecordsPreviousHash(t *testing.T) {
	userRepo, sessionRepo, issuer, shared := newRefreshFixture()
	uc := NewRefreshTokenUseCase(userRepo, issuer, sessionRepo, shared)

	pair, err := uc.Execute("orig", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair.RefreshToken != "raw-1" {
		t.Errorf("expected new token raw-1, got %s", pair.RefreshToken)
	}
	sess := sessionRepo.sessions["sess1"]
	if sess.PreviousRefreshTokenHash != issuer.HashRefreshToken("orig") {
		t.Error("rotation should record the previous token hash")
	}
	if sess.RotatedAt == nil {
		t.Error("rotation should stamp rotated_at")
	}
}

// The core P0.3 fix: replaying a spent refresh token after the grace window is
// treated as theft. Every session for the user is revoked and their live access
// JTIs are blacklisted so a stolen token can't survive to its JWT expiry.
func TestRefresh_ReuseOutsideGraceRevokesFamily(t *testing.T) {
	userRepo, sessionRepo, issuer, shared := newRefreshFixture()

	// A second live device for the same user: it must be torn down too.
	other := &auth.Session{
		ID:               "sess2",
		UserID:           "u1",
		RefreshTokenHash: "hashed-other",
		AccessJTI:        "jti-other",
		ExpiresAt:        time.Now().Add(24 * time.Hour),
	}
	sessionRepo.sessions["sess2"] = other
	sessionRepo.byHash["hashed-other"] = other

	uc := NewRefreshTokenUseCase(userRepo, issuer, sessionRepo, shared)

	if _, err := uc.Execute("orig", "", ""); err != nil {
		t.Fatalf("first rotation failed: %v", err)
	}
	// Push the rotation into the past so the replay is unambiguously outside grace.
	sessionRepo.sessions["sess1"].RotatedAt = timePtr(time.Now().Add(-2 * refreshGraceWindow))

	_, err := uc.Execute("orig", "", "")
	if !errors.Is(err, auth.ErrRefreshTokenReuse) {
		t.Fatalf("expected ErrRefreshTokenReuse, got: %v", err)
	}
	if !sessionRepo.sessions["sess1"].IsRevoked() {
		t.Error("the reused session must be revoked")
	}
	if !sessionRepo.sessions["sess2"].IsRevoked() {
		t.Error("the whole family (every user session) must be revoked")
	}
	if ok, _ := shared.Exists(revokedJTIPrefix + "jti-other"); !ok {
		t.Error("the other session's access JTI must be blacklisted")
	}
	if userRepo.byID["u1"].TokenVersion == 0 {
		t.Error("token version should bump so cached role/version checks fail")
	}
}

// An honest client whose refresh response was lost retries with the same token.
// Inside the grace window that must re-rotate cleanly, not nuke the account.
func TestRefresh_ReuseInsideGraceReRotates(t *testing.T) {
	userRepo, sessionRepo, issuer, shared := newRefreshFixture()
	uc := NewRefreshTokenUseCase(userRepo, issuer, sessionRepo, shared)

	if _, err := uc.Execute("orig", "", ""); err != nil {
		t.Fatalf("first rotation failed: %v", err)
	}
	// rotated_at is 'now' from the rotation above, so the replay is within grace.
	pair, err := uc.Execute("orig", "", "")
	if err != nil {
		t.Fatalf("expected honest retry to succeed, got: %v", err)
	}
	if pair == nil || pair.RefreshToken == "" {
		t.Fatal("expected a fresh token pair from the grace re-rotation")
	}
	if sessionRepo.sessions["sess1"].IsRevoked() {
		t.Error("a within-grace retry must not revoke the session")
	}
}

// A token that matches neither a live session nor any previous hash is just bogus.
func TestRefresh_UnknownTokenIsInvalid(t *testing.T) {
	userRepo, sessionRepo, issuer, shared := newRefreshFixture()
	uc := NewRefreshTokenUseCase(userRepo, issuer, sessionRepo, shared)

	_, err := uc.Execute("never-issued", "", "")
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got: %v", err)
	}
}

// Reuse detection must still revoke the family even if the Redis layer is absent.
func TestRefresh_ReuseWithoutSharedStillRevokes(t *testing.T) {
	userRepo, sessionRepo, issuer, _ := newRefreshFixture()
	uc := NewRefreshTokenUseCase(userRepo, issuer, sessionRepo, nil)

	if _, err := uc.Execute("orig", "", ""); err != nil {
		t.Fatalf("first rotation failed: %v", err)
	}
	sessionRepo.sessions["sess1"].RotatedAt = timePtr(time.Now().Add(-2 * refreshGraceWindow))

	_, err := uc.Execute("orig", "", "")
	if !errors.Is(err, auth.ErrRefreshTokenReuse) {
		t.Fatalf("expected ErrRefreshTokenReuse, got: %v", err)
	}
	if !sessionRepo.sessions["sess1"].IsRevoked() {
		t.Error("session must be revoked even without a shared cache")
	}
}
