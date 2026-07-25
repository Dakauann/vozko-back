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

	// Delete only removes the local record; it does NOT deregister at Meta.
	// Refuse to delete a CONNECTED number so we can never leave it orphaned
	// (still live at Meta, webhooks firing) while it vanishes from our view.
	// Callers must Deregister or Remove (release) a connected number first.
	phone, err := uc.repo.FindByID(phoneID)
	if err != nil {
		return err
	}
	if phone.Status == businessphone.StatusConnected {
		return businessphone.ErrPhoneNumberStillConnected
	}

	return uc.repo.Delete(phoneID)
}
