package businessphone_usecase

import (
	"context"

	"vozko/domain/conversation"
	businessphone "vozko/domain/whatsapp/business_phone"
)

type updateBusinessProfileUseCase struct {
	repo       businessphone.Repository
	metaClient businessphone.MetaAPIService
	// factory builds the 360dialog channel client for numbers that carry no Meta token.
	factory conversation.WhatsAppClientFactory
}

func NewUpdateBusinessProfileUseCase(repo businessphone.Repository, metaClient businessphone.MetaAPIService, factory conversation.WhatsAppClientFactory) businessphone.UpdateBusinessProfileUseCase {
	return &updateBusinessProfileUseCase{
		repo:       repo,
		metaClient: metaClient,
		factory:    factory,
	}
}

func (uc *updateBusinessProfileUseCase) Execute(input businessphone.UpdateBusinessProfileInput) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	if input.PhoneID == "" {
		return nil, businessphone.ErrPhoneNumberNotFound
	}

	if !input.Profile.Vertical.IsValid() {
		return nil, businessphone.ErrInvalidBusinessVertical
	}

	phone, err := uc.repo.FindByID(input.PhoneID)
	if err != nil {
		return nil, err
	}

	if phone.Provider.IsDialog360() {
		// dialog360 numbers write the profile through the channel API (no Meta token).
		if err := uc.updateDialog360Profile(phone.ID, input.Profile); err != nil {
			return nil, err
		}
		phone.BusinessProfile = input.Profile
		if err := uc.repo.UpdateBusinessProfile(phone.ID, input.Profile); err != nil {
			return nil, err
		}
		return phone, nil
	}

	if input.AccessToken == "" {
		return nil, businessphone.ErrInvalidAccessToken
	}

	metaProfile := businessphone.MetaBusinessProfile{
		About:                input.Profile.About,
		Address:              input.Profile.Address,
		Description:          input.Profile.Description,
		Email:                input.Profile.Email,
		ProfilePictureURL:    input.Profile.ProfilePictureURL,
		ProfilePictureHandle: input.Profile.ProfilePictureHandle,
		Websites:             input.Profile.Websites,
		Vertical:             string(input.Profile.Vertical),
	}

	err = uc.metaClient.UpdateBusinessProfile(phone.MetaPhoneNumberID, metaProfile, input.AccessToken)
	if err != nil {
		return nil, err
	}

	phone.BusinessProfile = input.Profile

	if err := uc.repo.UpdateBusinessProfile(phone.ID, input.Profile); err != nil {
		return nil, err
	}

	token := input.AccessToken
	if token == "" {
		token = phone.AccessToken
	}
	if token != "" {
		metaPhone, err := uc.metaClient.GetPhoneNumber(phone.MetaPhoneNumberID, token)
		if err == nil && metaPhone != nil {
			phone.VerifiedName = metaPhone.VerifiedName
			phone.Status = businessphone.Status(metaPhone.Status)
			phone.QualityRating = businessphone.QualityRating(metaPhone.QualityRating)
			phone.NameStatus = businessphone.NameStatus(metaPhone.NameStatus)
			phone.CodeVerificationStatus = metaPhone.CodeVerificationStatus
			phone.IsOfficialBusiness = metaPhone.IsOfficialBusiness
			_ = uc.repo.Update(phone.ID, phone)
		}
	}

	return phone, nil
}

func (uc *updateBusinessProfileUseCase) updateDialog360Profile(phoneID string, profile businessphone.BusinessProfile) error {
	if uc.factory == nil {
		return businessphone.ErrUnsupportedForProvider
	}
	client, err := uc.factory.ClientForPhone(phoneID)
	if err != nil {
		return err
	}
	bp, ok := client.(conversation.WhatsAppBusinessProfileClient)
	if !ok {
		return businessphone.ErrUnsupportedForProvider
	}
	return bp.UpdateBusinessProfile(context.Background(), conversation.WhatsAppBusinessProfile{
		About:                profile.About,
		Address:              profile.Address,
		Description:          profile.Description,
		Email:                profile.Email,
		ProfilePictureURL:    profile.ProfilePictureURL,
		ProfilePictureHandle: profile.ProfilePictureHandle,
		Websites:             profile.Websites,
		Vertical:             string(profile.Vertical),
	})
}
