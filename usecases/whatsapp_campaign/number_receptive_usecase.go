package whatsapp_campaign_usecase

import (
	"errors"

	businessphone "vozko/domain/whatsapp/business_phone"
	wc "vozko/domain/whatsapp_campaign"
)

type ReceptivePhones interface {
	FindByID(id string) (*businessphone.WhatsAppBusinessPhoneNumber, error)
}

type NumberReceptive struct {
	phones     ReceptivePhones
	containers wc.Repository
	ensure     wc.EnsureReceptiveContainerUseCase
}

func NewNumberReceptive(phones ReceptivePhones, containers wc.Repository, ensure wc.EnsureReceptiveContainerUseCase) *NumberReceptive {
	return &NumberReceptive{phones: phones, containers: containers, ensure: ensure}
}

func (uc *NumberReceptive) Get(workspaceID, phoneID string) (wc.ReceptiveSettings, error) {
	phone, err := uc.ownedPhone(workspaceID, phoneID)
	if err != nil {
		return wc.ReceptiveSettings{}, err
	}
	container, err := uc.containers.FindLatestOrganicByBusinessPhone(phone.OwnerWorkspaceID, phone.ID)
	if errors.Is(err, wc.ErrCampaignNotFound) {
		return wc.ReceptiveSettings{}, nil
	}
	if err != nil {
		return wc.ReceptiveSettings{}, err
	}
	return container.Receptive(), nil
}

func (uc *NumberReceptive) Update(workspaceID, phoneID string, settings wc.ReceptiveSettings) (wc.ReceptiveSettings, error) {
	phone, err := uc.ownedPhone(workspaceID, phoneID)
	if err != nil {
		return wc.ReceptiveSettings{}, err
	}
	if _, _, err := uc.ensure.Execute(phone.OwnerWorkspaceID, phone.ID, phone.DisplayPhoneNumber); err != nil {
		return wc.ReceptiveSettings{}, err
	}
	settings.Normalize()
	if _, err := uc.containers.UpdateReceptive(phone.OwnerWorkspaceID, phone.ID, settings); err != nil {
		return wc.ReceptiveSettings{}, err
	}
	return settings, nil
}

func (uc *NumberReceptive) ownedPhone(workspaceID, phoneID string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	phone, err := uc.phones.FindByID(phoneID)
	if errors.Is(err, businessphone.ErrPhoneNumberNotFound) || (err == nil && phone == nil) {
		return nil, wc.ErrCampaignBusinessPhoneNotFound
	}
	if err != nil {
		return nil, err
	}
	if !phone.BelongsToWorkspace(workspaceID) {
		return nil, wc.ErrReceptiveNotOwner
	}
	return phone, nil
}
