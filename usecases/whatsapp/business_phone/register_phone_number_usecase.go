package businessphone_usecase

import (
	"fmt"
	"log"

	businessphone "vozko/domain/whatsapp/business_phone"
)

type registerPhoneUseCase struct {
	repo       businessphone.Repository
	metaClient businessphone.MetaAPIService
}

func NewRegisterPhoneUseCase(repo businessphone.Repository, metaClient businessphone.MetaAPIService) businessphone.RegisterPhoneUseCase {
	return &registerPhoneUseCase{
		repo:       repo,
		metaClient: metaClient,
	}
}

func (uc *registerPhoneUseCase) Execute(input businessphone.RegisterPhoneInput) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	if input.PhoneID == "" {
		return nil, businessphone.ErrPhoneNumberNotFound
	}
	if input.Pin == "" {
		return nil, fmt.Errorf("%w: 6-digit PIN is required for registration", businessphone.ErrInvalidVerificationCode)
	}
	if len(input.Pin) != 6 {
		return nil, fmt.Errorf("%w: PIN must be exactly 6 digits", businessphone.ErrInvalidVerificationCode)
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

	if phone.Status == businessphone.StatusConnected {
		return nil, businessphone.ErrPhoneNumberAlreadyRegistered
	}

	log.Printf("[register-phone] Registering phone %s (meta_id=%s) with Cloud API", phone.ID, phone.MetaPhoneNumberID)

	err = uc.metaClient.RegisterPhone(phone.MetaPhoneNumberID, input.Pin, input.AccessToken)
	if err != nil {
		log.Printf("[register-phone] Failed to register phone %s: %v", phone.ID, err)
		return nil, err
	}

	metaPhone, err := uc.metaClient.GetPhoneNumber(phone.MetaPhoneNumberID, input.AccessToken)
	if err != nil {
		log.Printf("[register-phone] Registered but failed to sync status: %v (marking as CONNECTED)", err)
		phone.Status = businessphone.StatusConnected
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

	log.Printf("[register-phone] Phone %s registered successfully (status=%s)", phone.ID, phone.Status)
	return phone, nil
}
