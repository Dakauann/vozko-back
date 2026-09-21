package auth_usecase

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/auth"
)

func TestRefresh_ConcurrentRotationThenStaleCookieNukesHonestUser(t *testing.T) {
	userRepo, sessionRepo, issuer, shared := newRefreshFixture()

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

	pairA, err := uc.Execute("orig", "", "")
	if err != nil {
		t.Fatalf("tab A rotation failed: %v", err)
	}
	pairB, err := uc.Execute("orig", "", "")
	if err != nil {
		t.Fatalf("tab B grace re-rotation failed: %v", err)
	}
	if pairA.RefreshToken == pairB.RefreshToken {
		t.Fatal("test setup broken: both rotations returned the same token")
	}

	sessionRepo.sessions["sess1"].RotatedAt = timePtr(time.Now().Add(-2 * refreshGraceWindow))

	_, err = uc.Execute(pairA.RefreshToken, "", "")

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

func TestRefresh_LostResponseThenLaterRefreshNukesHonestUser(t *testing.T) {
	userRepo, sessionRepo, issuer, shared := newRefreshFixture()
	uc := NewRefreshTokenUseCase(userRepo, issuer, sessionRepo, shared)

	if _, err := uc.Execute("orig", "", ""); err != nil {
		t.Fatalf("rotation failed: %v", err)
	}

	sessionRepo.sessions["sess1"].RotatedAt = timePtr(time.Now().Add(-2 * refreshGraceWindow))

	_, err := uc.Execute("orig", "", "")
	if !errors.Is(err, auth.ErrRefreshTokenReuse) {
		t.Fatalf("replication expected ErrRefreshTokenReuse (the bug), got: %v", err)
	}
	if userRepo.byID["u1"].TokenVersion == 0 {
		t.Error("expected token version bump: only a fresh login recovers")
	}
}
