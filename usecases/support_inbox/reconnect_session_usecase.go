package support_inbox_usecase

import (
	si "vozko/domain/support_inbox"
)

type reconnectSessionUseCase struct {
	inboxRepo   si.Repository
	sessionRepo si.SessionRepository
	tokenSecret string
}

func NewReconnectSessionUseCase(
	inboxRepo si.Repository,
	sessionRepo si.SessionRepository,
	tokenSecret string,
) si.ReconnectSessionUseCase {
	return &reconnectSessionUseCase{
		inboxRepo:   inboxRepo,
		sessionRepo: sessionRepo,
		tokenSecret: tokenSecret,
	}
}

func (uc *reconnectSessionUseCase) Execute(input si.ReconnectSessionInput) (*si.ReconnectSessionOutput, error) {

	if !si.ValidateSessionToken(input.SessionToken, uc.tokenSecret) {
		return nil, si.ErrSessionInvalid
	}

	session, err := uc.sessionRepo.FindByToken(input.SessionToken)
	if err != nil {
		return nil, err
	}

	if session.IsExpired() {
		return nil, si.ErrSessionExpired
	}

	inbox, err := uc.inboxRepo.FindByID(session.InboxID)
	if err != nil {
		return nil, err
	}

	if input.Origin != "" && !inbox.IsOriginAllowed(input.Origin) {
		return nil, si.ErrSessionInvalid
	}

	return &si.ReconnectSessionOutput{
		EntryID:         session.EntryID,
		InboxID:         session.InboxID,
		GreetingMessage: inbox.GreetingMessage,
		ExpiresAt:       session.ExpiresAt,
	}, nil
}
