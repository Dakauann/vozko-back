package businessphone_usecase

import (
	businessphone "vozko/domain/whatsapp/business_phone"
)

type getUseCase struct {
	repo businessphone.Repository
}

func NewGetUseCase(repo businessphone.Repository) businessphone.GetUseCase {
	return &getUseCase{repo: repo}
}

func (uc *getUseCase) Execute(phoneID string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	if phoneID == "" {
		return nil, businessphone.ErrPhoneNumberNotFound
	}
	return uc.repo.FindByID(phoneID)
}
