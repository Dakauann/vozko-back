package businessphone_usecase

import (
	"strings"

	businessphone "vozko/domain/whatsapp/business_phone"
)

type verifyCodeUseCase struct {
	repo       businessphone.Repository
	metaClient businessphone.MetaAPIService
}

func NewVerifyCodeUseCase(repo businessphone.Repository, metaClient businessphone.MetaAPIService) businessphone.VerifyCodeUseCase {
	return &verifyCodeUseCase{
		repo:       repo,
		metaClient: metaClient,
	}
}

func (uc *verifyCodeUseCase) Execute(input businessphone.VerifyCodeInput) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	if input.PhoneID == "" {
		return nil, businessphone.ErrPhoneNumberNotFound
	}
	if input.Code == "" {
		return nil, businessphone.ErrInvalidVerificationCode
	}

	phone, err := uc.repo.FindByID(input.PhoneID)
	if err != nil {
		return nil, err
	}

	accessToken := strings.TrimSpace(phone.AccessToken)
	if accessToken == "" {
		accessToken = strings.TrimSpace(input.AccessToken)
	}
	if accessToken == "" {
		return nil, businessphone.ErrInvalidAccessToken
	}

	success, err := uc.metaClient.VerifyCode(phone.MetaPhoneNumberID, input.Code, accessToken)
	if err != nil {
		return nil, err
	}

	if !success {
		return nil, businessphone.ErrVerificationFailed
	}

	phone.CodeVerificationStatus = "VERIFIED"
	phone.Status = businessphone.StatusConnected

	if err := uc.repo.Update(phone.ID, phone); err != nil {
		return nil, err
	}

	return phone, nil
}
