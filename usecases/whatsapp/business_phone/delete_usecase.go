package businessphone_usecase

import (
	businessphone "vozko/domain/whatsapp/business_phone"
)

type deletePhoneNumberUseCase struct {
	repo businessphone.Repository
}

func NewDeletePhoneNumberUseCase(repo businessphone.Repository) businessphone.DeletePhoneNumberUseCase {
	return &deletePhoneNumberUseCase{repo: repo}
}

func (uc *deletePhoneNumberUseCase) Execute(phoneID string) error {
	if phoneID == "" {
		return businessphone.ErrPhoneNumberNotFound
	}

	phone, err := uc.repo.FindByID(phoneID)
	if err != nil {
		return err
	}
	if phone.Status == businessphone.StatusConnected {
		return businessphone.ErrPhoneNumberStillConnected
	}

	return uc.repo.Delete(phoneID)
}
