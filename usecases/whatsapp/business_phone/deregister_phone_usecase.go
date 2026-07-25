package businessphone_usecase

import (
	"log"

	businessphone "vozko/domain/whatsapp/business_phone"
)

type deregisterPhoneUseCase struct {
	repo       businessphone.Repository
	metaClient businessphone.MetaAPIService
}

func NewDeregisterPhoneUseCase(repo businessphone.Repository, metaClient businessphone.MetaAPIService) businessphone.DeregisterPhoneUseCase {
	return &deregisterPhoneUseCase{
		repo:       repo,
		metaClient: metaClient,
	}
}

func (uc *deregisterPhoneUseCase) Execute(input businessphone.DeregisterPhoneInput) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
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

	if phone.MetaPhoneNumberID == "" {
		return nil, businessphone.ErrMetaPhoneNumberIDRequired
	}

	if phone.Status != businessphone.StatusConnected {
		return nil, businessphone.ErrPhoneNumberNotConnected
	}

	log.Printf("[deregister-phone] Deregistering phone %s (meta_id=%s) from Cloud API", phone.ID, phone.MetaPhoneNumberID)

	err = uc.metaClient.DeregisterPhone(phone.MetaPhoneNumberID, input.AccessToken)
	if err != nil {
		log.Printf("[deregister-phone] Failed to deregister phone %s: %v", phone.ID, err)
		return nil, err
	}

	metaPhone, err := uc.metaClient.GetPhoneNumber(phone.MetaPhoneNumberID, input.AccessToken)
	if err != nil {
		log.Printf("[deregister-phone] Deregistered but failed to sync status: %v (marking as DISCONNECTED)", err)
		phone.Status = businessphone.StatusDisconnected
	} else {
		phone.VerifiedName = metaPhone.VerifiedName
		phone.Status = businessphone.Status(metaPhone.Status)
		phone.QualityRating = businessphone.QualityRating(metaPhone.QualityRating)
		phone.NameStatus = businessphone.NameStatus(metaPhone.NameStatus)
		phone.CodeVerificationStatus = metaPhone.CodeVerificationStatus
		phone.IsOfficialBusiness = metaPhone.IsOfficialBusiness
	}

	if err := uc.repo.Update(phone.ID, phone); err != nil {
		return nil, err
	}

	log.Printf("[deregister-phone] Phone %s deregistered successfully (status=%s)", phone.ID, phone.Status)
	return phone, nil
}
