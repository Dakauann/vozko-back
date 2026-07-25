package auth_usecase

import (
	"vozko/domain/auth"
	"vozko/domain/cache"
	"vozko/domain/user"
)

type logoutAllUseCase struct {
	userRepo    user.UserRepository
	sessionRepo auth.SessionRepository
	shared      cache.SharedState
}

func NewLogoutAllUseCase(userRepo user.UserRepository, sessionRepo auth.SessionRepository, shared cache.SharedState) auth.LogoutAllUseCase {
	return &logoutAllUseCase{userRepo: userRepo, sessionRepo: sessionRepo, shared: shared}
}

func (uc *logoutAllUseCase) Execute(userID string) error {

	sessions, _ := uc.sessionRepo.FindActiveByUserID(userID)

	if err := uc.sessionRepo.RevokeAllByUserID(userID); err != nil {
		return err
	}

	for _, s := range sessions {
		if s.AccessJTI != "" {
			_ = uc.shared.SetString(revokedJTIPrefix+s.AccessJTI, "1", revokedJTITTL)
		}
	}

	_, _ = uc.userRepo.IncrementTokenVersion(userID)

	_ = uc.shared.Del("cache:token_ver:" + userID)

	return nil
}
