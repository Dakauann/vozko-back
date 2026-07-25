package support_inbox_usecase

import si "vozko/domain/support_inbox"

type updateInboxUseCase struct {
	repo si.Repository
}

func NewUpdateInboxUseCase(repo si.Repository) si.UpdateInboxUseCase {
	return &updateInboxUseCase{repo: repo}
}

func (uc *updateInboxUseCase) Execute(id string, inbox *si.SupportInbox) (*si.SupportInbox, error) {
	existing, err := uc.repo.FindByID(id)
	if err != nil {
		return nil, err
	}

	inbox.Normalize()
	if err := inbox.Validate(); err != nil {
		return nil, err
	}

	inbox.ID = existing.ID
	inbox.WorkspaceID = existing.WorkspaceID
	inbox.CreatedAt = existing.CreatedAt

	if err := uc.repo.Update(id, inbox); err != nil {
		return nil, err
	}

	updated, err := uc.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	return updated, nil
}
