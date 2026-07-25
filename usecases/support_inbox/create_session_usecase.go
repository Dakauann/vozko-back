package support_inbox_usecase

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	si "vozko/domain/support_inbox"
)

type createSessionUseCase struct {
	inboxRepo   si.Repository
	entryRepo   si.EntryRepository
	sessionRepo si.SessionRepository
	tokenSecret string
}

func NewCreateSessionUseCase(
	inboxRepo si.Repository,
	entryRepo si.EntryRepository,
	sessionRepo si.SessionRepository,
	tokenSecret string,
) si.CreateSessionUseCase {
	return &createSessionUseCase{
		inboxRepo:   inboxRepo,
		entryRepo:   entryRepo,
		sessionRepo: sessionRepo,
		tokenSecret: tokenSecret,
	}
}

func (uc *createSessionUseCase) Execute(input si.CreateSessionInput) (*si.CreateSessionOutput, error) {
	inbox, err := uc.inboxRepo.FindByID(input.InboxID)
	if err != nil {
		return nil, err
	}

	if len(inbox.AllowedOrigins) > 0 && !inbox.IsOriginAllowed(input.Origin) {
		return nil, fmt.Errorf("origin %q is not allowed for this support inbox", input.Origin)
	}

	entry := &si.SupportEntry{
		ID:           uuid.New().String(),
		InboxID:      inbox.ID,
		ContactName:  input.ContactName,
		ContactEmail: input.ContactEmail,
		SourceURL:    input.SourceURL,
	}
	entry.Normalize()
	if err := uc.entryRepo.Create(entry); err != nil {
		return nil, err
	}

	token, err := si.GenerateSessionToken(uc.tokenSecret)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	session := &si.SupportSession{
		ID:        uuid.New().String(),
		InboxID:   inbox.ID,
		EntryID:   entry.ID,
		Token:     token,
		ExpiresAt: now.Add(si.SessionTTL),
		CreatedAt: now,
	}
	if err := uc.sessionRepo.Create(session); err != nil {
		return nil, err
	}

	return &si.CreateSessionOutput{
		SessionToken:    token,
		EntryID:         entry.ID,
		GreetingMessage: inbox.GreetingMessage,
		PreChatFields:   inbox.PreChatFields,
		ExpiresAt:       session.ExpiresAt,
	}, nil
}
