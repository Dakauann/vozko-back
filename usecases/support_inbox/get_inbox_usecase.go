package support_inbox_usecase

import si "vozko/domain/support_inbox"

type getInboxUseCase struct {
	repo si.Repository
}

func NewGetInboxUseCase(repo si.Repository) si.GetInboxUseCase {
	return &getInboxUseCase{repo: repo}
}

func (uc *getInboxUseCase) Execute(id string) (*si.SupportInbox, error) {
	return uc.repo.FindByID(id)
}
