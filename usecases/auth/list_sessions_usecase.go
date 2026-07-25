package auth_usecase

import "vozko/domain/auth"

type listSessionsUseCase struct {
	sessionRepo auth.SessionRepository
}

func NewListSessionsUseCase(sessionRepo auth.SessionRepository) auth.ListSessionsUseCase {
	return &listSessionsUseCase{sessionRepo: sessionRepo}
}

func (uc *listSessionsUseCase) Execute(userID string) ([]*auth.Session, error) {
	return uc.sessionRepo.FindActiveByUserID(userID)
}
