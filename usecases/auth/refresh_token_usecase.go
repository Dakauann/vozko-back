package auth_usecase

import (
	"log"
	"time"

	"vozko/domain/auth"
	"vozko/domain/cache"
	"vozko/domain/user"
)

// refreshGraceWindow tolerates an honest client replaying the just-rotated token,
// e.g. when the response carrying the new token was lost and the client retried.
// Inside it a superseded token is re-rotated instead of treated as theft; outside
// it, presenting a spent token trips reuse detection.
const refreshGraceWindow = 30 * time.Second

type refreshTokenUseCase struct {
	userRepo    user.UserRepository
	tokenIssuer auth.TokenIssuer
	sessionRepo auth.SessionRepository
	shared      cache.SharedState
}

func NewRefreshTokenUseCase(userRepo user.UserRepository, tokenIssuer auth.TokenIssuer, sessionRepo auth.SessionRepository, shared cache.SharedState) auth.RefreshTokenUseCase {
	return &refreshTokenUseCase{
		userRepo:    userRepo,
		tokenIssuer: tokenIssuer,
		sessionRepo: sessionRepo,
		shared:      shared,
	}
}

func (uc *refreshTokenUseCase) Execute(refreshToken string, ipAddress string, deviceInfo string) (*auth.TokenPair, error) {
	hash := uc.tokenIssuer.HashRefreshToken(refreshToken)

	session, err := uc.sessionRepo.FindByRefreshTokenHash(hash)
	if err == nil {
		if session.IsRevoked() {
			return nil, auth.ErrSessionRevoked
		}
		if session.IsExpired() {
			return nil, auth.ErrSessionExpired
		}
		pair, rotateErr := uc.rotate(session, hash, ipAddress, deviceInfo)
		if rotateErr != errRotationConflict {
			return pair, rotateErr
		}
		// Lost the CAS race: another request rotated this exact token between our
		// read and write. Fall through and treat our now-stale hash as a previous
		// token (honest concurrent refresh), which the grace path re-rotates.
	}

	// The presented token is not a live one. It may be an honest retry of a token
	// that was just rotated (grace) or a replay of a long-spent token (theft).
	prev, prevErr := uc.sessionRepo.FindByPreviousRefreshTokenHash(hash)
	if prevErr != nil || prev == nil {
		return nil, auth.ErrInvalidCredentials
	}
	if prev.IsRevoked() {
		return nil, auth.ErrSessionRevoked
	}

	withinGrace := prev.RotatedAt != nil && time.Since(*prev.RotatedAt) <= refreshGraceWindow
	if !withinGrace {
		// A spent refresh token replayed outside the grace window: assume theft and
		// tear down every session for the user, blacklisting their access tokens so
		// the stolen credentials die immediately rather than at JWT expiry.
		uc.handleReuse(prev)
		return nil, auth.ErrRefreshTokenReuse
	}

	if prev.IsExpired() {
		return nil, auth.ErrSessionExpired
	}
	pair, rotateErr := uc.rotate(prev, prev.RefreshTokenHash, ipAddress, deviceInfo)
	if rotateErr == errRotationConflict {
		return nil, auth.ErrInvalidCredentials
	}
	return pair, rotateErr
}

// errRotationConflict signals that the compare-and-swap matched no row because a
// concurrent request rotated the token first. It never leaves this file.
var errRotationConflict = auth.ErrSessionNotFound

func (uc *refreshTokenUseCase) rotate(session *auth.Session, expectedHash, ipAddress, deviceInfo string) (*auth.TokenPair, error) {
	u, err := uc.userRepo.FindByID(session.UserID)
	if err != nil {
		return nil, auth.ErrInvalidCredentials
	}

	pair, err := uc.tokenIssuer.Issue(u)
	if err != nil {
		return nil, err
	}

	newRaw, newHash, err := uc.tokenIssuer.GenerateRefreshToken()
	if err != nil {
		return nil, err
	}

	affected, err := uc.sessionRepo.UpdateRefreshToken(session.ID, expectedHash, newHash, pair.AccessJTI, session.ExpiresAt)
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, errRotationConflict
	}

	if ipAddress != "" || deviceInfo != "" {
		_ = uc.sessionRepo.UpdateSessionInfo(session.ID, ipAddress, deviceInfo)
	}

	pair.RefreshToken = newRaw
	return pair, nil
}

// handleReuse responds to a detected stolen-token replay by revoking every session
// for the user (RFC 9700's family invalidation, taken to the whole account since we
// don't track token lineage) and blacklisting their live access-token JTIs so the
// compromise can't ride an unexpired JWT.
func (uc *refreshTokenUseCase) handleReuse(reused *auth.Session) {
	log.Printf("REFRESH TOKEN REUSE detected for user %s (session %s); revoking all sessions", reused.UserID, reused.ID)

	if uc.shared != nil {
		sessions, _ := uc.sessionRepo.FindActiveByUserID(reused.UserID)
		for _, s := range sessions {
			if s.AccessJTI != "" {
				_ = uc.shared.SetString(revokedJTIPrefix+s.AccessJTI, "1", revokedJTITTL)
			}
		}
	}

	_ = uc.sessionRepo.RevokeAllByUserID(reused.UserID)
	_, _ = uc.userRepo.IncrementTokenVersion(reused.UserID)
	if uc.shared != nil {
		_ = uc.shared.Del("cache:token_ver:" + reused.UserID)
	}
}
