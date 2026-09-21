package auth_usecase

import (
	"log"
	"time"

	"vozko/domain/auth"
	"vozko/domain/cache"
	"vozko/domain/user"
)

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
	}

	prev, prevErr := uc.sessionRepo.FindByPreviousRefreshTokenHash(hash)
	if prevErr != nil || prev == nil {
		return nil, auth.ErrInvalidCredentials
	}
	if prev.IsRevoked() {
		return nil, auth.ErrSessionRevoked
	}

	withinGrace := prev.RotatedAt != nil && time.Since(*prev.RotatedAt) <= refreshGraceWindow
	if !withinGrace {
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
