package businessphone_usecase

import (
	"context"

	"vozko/domain/conversation"
	businessphone "vozko/domain/whatsapp/business_phone"
)

type getBusinessProfileUseCase struct {
	repo       businessphone.Repository
	metaClient businessphone.MetaAPIService
	// factory builds the 360dialog channel client for numbers that carry no Meta token.
	factory conversation.WhatsAppClientFactory
}

func NewGetBusinessProfileUseCase(repo businessphone.Repository, metaClient businessphone.MetaAPIService, factory conversation.WhatsAppClientFactory) businessphone.GetBusinessProfileUseCase {
	return &getBusinessProfileUseCase{
		repo:       repo,
		metaClient: metaClient,
		factory:    factory,
	}
}

func (uc *getBusinessProfileUseCase) Execute(input businessphone.GetBusinessProfileInput) (*businessphone.BusinessProfile, error) {
	if input.PhoneID == "" {
		return nil, businessphone.ErrPhoneNumberNotFound
	}

	phone, err := uc.repo.FindByID(input.PhoneID)
	if err != nil {
		return nil, err
	}

	var profile businessphone.BusinessProfile
	if phone.Provider.IsDialog360() {
		// dialog360 numbers have no Meta token; read the profile via the channel API.
		p, err := uc.dialog360Profile(phone.ID)
		if err != nil {
			return nil, err
		}
		profile = *p
	} else {
		if input.AccessToken == "" {
			return nil, businessphone.ErrInvalidAccessToken
		}
		metaProfile, err := uc.metaClient.GetBusinessProfile(phone.MetaPhoneNumberID, input.AccessToken)
		if err != nil {
			return nil, err
		}
		profile = businessphone.BusinessProfile{
			About:             metaProfile.About,
			Address:           metaProfile.Address,
			Description:       metaProfile.Description,
			Email:             metaProfile.Email,
			ProfilePictureURL: metaProfile.ProfilePictureURL,
			Websites:          metaProfile.Websites,
			Vertical:          businessphone.BusinessVertical(metaProfile.Vertical),
		}
	}

	if err := uc.repo.UpdateBusinessProfile(phone.ID, profile); err != nil {
		return nil, err
	}

	return &profile, nil
}

func (uc *getBusinessProfileUseCase) dialog360Profile(phoneID string) (*businessphone.BusinessProfile, error) {
	if uc.factory == nil {
		return nil, businessphone.ErrUnsupportedForProvider
	}
	client, err := uc.factory.ClientForPhone(phoneID)
	if err != nil {
		return nil, err
	}
	bp, ok := client.(conversation.WhatsAppBusinessProfileClient)
	if !ok {
		return nil, businessphone.ErrUnsupportedForProvider
	}
	p, err := bp.GetBusinessProfile(context.Background())
	if err != nil {
		return nil, err
	}
	return &businessphone.BusinessProfile{
		About:             p.About,
		Address:           p.Address,
		Description:       p.Description,
		Email:             p.Email,
		ProfilePictureURL: p.ProfilePictureURL,
		Websites:          p.Websites,
		Vertical:          businessphone.BusinessVertical(p.Vertical),
	}, nil
}
