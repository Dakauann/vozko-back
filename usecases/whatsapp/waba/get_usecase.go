package waba_usecase

import "vozko/domain/whatsapp/waba"

type getUseCase struct {
	repo waba.Repository
}

func NewGetUseCase(repo waba.Repository) waba.GetUseCase {
	return &getUseCase{repo: repo}
}

func (uc *getUseCase) Execute(id string) (*waba.WhatsAppBusinessAccount, error) {
	return uc.repo.FindByID(id)
}
