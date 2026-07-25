package auth_usecase

import (
	"vozko/domain/auth"
	"vozko/domain/cache"
)

type logoutUseCase struct {
	sessionRepo auth.SessionRepository
	shared      cache.SharedState
}

func NewLogoutUseCase(sessionRepo auth.SessionRepository, shared cache.SharedState) auth.LogoutUseCase {
	return &logoutUseCase{sessionRepo: sessionRepo, shared: shared}
}

func (uc *logoutUseCase) Execute(userID string, sessionID string, accessJTI string) error {
	var session *auth.Session
	var err error

	if sessionID != "" {
		session, err = uc.sessionRepo.FindByID(sessionID)
	} else if accessJTI != "" {

		session, err = uc.sessionRepo.FindByAccessJTI(userID, accessJTI)
	} else {
		return auth.ErrSessionNotFound
	}

	if err != nil {
		return auth.ErrSessionNotFound
	}

	if session.UserID != userID {
		return auth.ErrSessionNotFound
	}

	if err := uc.sessionRepo.Revoke(session.ID); err != nil {
		return err
	}

	if session.AccessJTI != "" {
		_ = uc.shared.SetString(revokedJTIPrefix+session.AccessJTI, "1", revokedJTITTL)
	}

	return nil
}
