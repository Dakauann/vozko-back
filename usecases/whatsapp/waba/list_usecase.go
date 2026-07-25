package waba_usecase

import (
	"vozko/domain/shared"
	"vozko/domain/whatsapp/waba"
)

type listUseCase struct {
	repo waba.Repository
}

func NewListUseCase(repo waba.Repository) waba.ListUseCase {
	return &listUseCase{repo: repo}
}

func (uc *listUseCase) Execute(input waba.ListInput) (*shared.PaginatedResult[*waba.WhatsAppBusinessAccount], error) {
	return uc.repo.List(input)
}
