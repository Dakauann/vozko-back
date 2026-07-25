package businessphone_usecase

import (
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
)

type listUseCase struct {
	repo businessphone.Repository
}

func NewListUseCase(repo businessphone.Repository) businessphone.ListUseCase {
	return &listUseCase{repo: repo}
}

func (uc *listUseCase) Execute(input businessphone.ListInput) (*shared.PaginatedResult[*businessphone.WhatsAppBusinessPhoneNumber], error) {
	return uc.repo.List(input)
}
