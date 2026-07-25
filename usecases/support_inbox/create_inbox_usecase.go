package support_inbox_usecase

import (
	"time"

	"github.com/google/uuid"

	si "vozko/domain/support_inbox"
)

type createInboxUseCase struct {
	repo si.Repository
}

func NewCreateInboxUseCase(repo si.Repository) si.CreateInboxUseCase {
	return &createInboxUseCase{repo: repo}
}

func (uc *createInboxUseCase) Execute(inbox *si.SupportInbox) (*si.SupportInbox, error) {
	inbox.Normalize()
	if err := inbox.Validate(); err != nil {
		return nil, err
	}
	now := time.Now()
	inbox.ID = uuid.New().String()
	inbox.Archived = false
	inbox.CreatedAt = now
	inbox.UpdatedAt = now

	if err := uc.repo.Create(inbox); err != nil {
		return nil, err
	}
	return inbox, nil
}
