package auth_usecase

import (
	"time"

	"vozko/domain/auth"
	"vozko/domain/cache"
)

const revokedJTIPrefix = "revoked_jti:"

const revokedJTITTL = 16 * time.Minute

type revokeSessionUseCase struct {
	sessionRepo auth.SessionRepository
	shared      cache.SharedState
}

func NewRevokeSessionUseCase(sessionRepo auth.SessionRepository, shared cache.SharedState) auth.RevokeSessionUseCase {
	return &revokeSessionUseCase{sessionRepo: sessionRepo, shared: shared}
}

func (uc *revokeSessionUseCase) Execute(userID string, sessionID string) error {
	session, err := uc.sessionRepo.FindByID(sessionID)
	if err != nil {
		return auth.ErrSessionNotFound
	}

	if session.UserID != userID {
		return auth.ErrSessionNotFound
	}

	if err := uc.sessionRepo.Revoke(sessionID); err != nil {
		return err
	}

	if session.AccessJTI != "" {
		_ = uc.shared.SetString(revokedJTIPrefix+session.AccessJTI, "1", revokedJTITTL)
	}

	return nil
}
