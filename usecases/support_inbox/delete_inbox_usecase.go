package support_inbox_usecase

import si "vozko/domain/support_inbox"

type deleteInboxUseCase struct {
	repo si.Repository
}

func NewDeleteInboxUseCase(repo si.Repository) si.DeleteInboxUseCase {
	return &deleteInboxUseCase{repo: repo}
}

func (uc *deleteInboxUseCase) Execute(id string) error {
	return uc.repo.Delete(id)
}
