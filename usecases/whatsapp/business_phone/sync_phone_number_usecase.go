package businessphone_usecase

import (
	businessphone "vozko/domain/whatsapp/business_phone"
)

type syncPhoneNumberUseCase struct {
	repo       businessphone.Repository
	metaClient businessphone.MetaAPIService
}

func NewSyncPhoneNumberUseCase(repo businessphone.Repository, metaClient businessphone.MetaAPIService) businessphone.SyncPhoneNumberUseCase {
	return &syncPhoneNumberUseCase{
		repo:       repo,
		metaClient: metaClient,
	}
}

func (uc *syncPhoneNumberUseCase) Execute(input businessphone.SyncPhoneNumberInput) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	if input.PhoneID == "" {
		return nil, businessphone.ErrPhoneNumberNotFound
	}
	if input.AccessToken == "" {
		return nil, businessphone.ErrInvalidAccessToken
	}

	phone, err := uc.repo.FindByID(input.PhoneID)
	if err != nil {
		return nil, err
	}

	metaPhone, err := uc.metaClient.GetPhoneNumber(phone.MetaPhoneNumberID, input.AccessToken)
	if err != nil {
		return nil, err
	}

	phone.VerifiedName = metaPhone.VerifiedName
	phone.Status = businessphone.Status(metaPhone.Status)
	phone.QualityRating = businessphone.QualityRating(metaPhone.QualityRating)
	phone.NameStatus = businessphone.NameStatus(metaPhone.NameStatus)
	phone.CodeVerificationStatus = metaPhone.CodeVerificationStatus
	phone.IsOfficialBusiness = metaPhone.IsOfficialBusiness

	if err := uc.repo.Update(phone.ID, phone); err != nil {
		return nil, err
	}

	return phone, nil
}
