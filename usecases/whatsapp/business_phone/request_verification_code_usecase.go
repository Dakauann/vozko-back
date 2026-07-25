package businessphone_usecase

import (
	"strings"

	businessphone "vozko/domain/whatsapp/business_phone"
)

type requestVerificationCodeUseCase struct {
	repo       businessphone.Repository
	metaClient businessphone.MetaAPIService
}

func NewRequestVerificationCodeUseCase(repo businessphone.Repository, metaClient businessphone.MetaAPIService) businessphone.RequestVerificationCodeUseCase {
	return &requestVerificationCodeUseCase{
		repo:       repo,
		metaClient: metaClient,
	}
}

func (uc *requestVerificationCodeUseCase) Execute(input businessphone.RequestVerificationCodeInput) error {
	if input.PhoneID == "" {
		return businessphone.ErrPhoneNumberNotFound
	}

	phone, err := uc.repo.FindByID(input.PhoneID)
	if err != nil {
		return err
	}

	accessToken := strings.TrimSpace(phone.AccessToken)
	if accessToken == "" {
		accessToken = strings.TrimSpace(input.AccessToken)
	}
	if accessToken == "" {
		return businessphone.ErrInvalidAccessToken
	}

	method := string(businessphone.VerificationMethodSMS)
	if input.Method != "" {
		method = string(input.Method)
	}

	err = uc.metaClient.RequestVerificationCode(phone.MetaPhoneNumberID, method, input.Language, accessToken)
	if err != nil {
		return err
	}

	phone.CodeVerificationStatus = "PENDING"
	if err := uc.repo.Update(phone.ID, phone); err != nil {
		return err
	}

	return nil
}
